package ui

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
)

// TestAgents: an agent's state shows on its issue's card; one that starts
// waiting notifies once; herdr going away clears the marks.
func TestAgents(t *testing.T) {
	m := jiraTabModel(t)
	agents := []herdr.Agent{{Name: "jira-abc-1-tbx", Status: herdr.Working}, {CWD: "/w/issue/ABC-2-fix", Status: herdr.Done}, {Name: "shell"}}
	out, cmd := m.handleAgents(agentsMsg{agents: agents})
	m = out.(Model)
	if cmd == nil || m.agents["ABC-1"][0].Status != herdr.Working || m.agents["ABC-2"][0].Status != herdr.Done {
		t.Fatalf("agents %v", m.agents)
	}
	card := ansi.Strip(strings.Join(m.jiraLaneCard(jira.Card{Key: "ABC-1", Summary: "First"}, false, 40), "\n"))
	if !strings.Contains(card, "⚙ ") {
		t.Errorf("card %q", card)
	}
	agents[0].Status = herdr.Blocked
	out, cmd = m.handleAgents(agentsMsg{agents: agents})
	m = out.(Model)
	if m.agentMark("ABC-1") == "" || cmd == nil {
		t.Fatal("blocked should mark and notify")
	}
	out, _ = m.handleAgents(agentsMsg{err: errors.New("no herdr")})
	if m = out.(Model); m.agentMark("ABC-1") != "" {
		t.Error("herdr gone: no marks")
	}
}

// TestAgentMarks: several agents on one issue show the worst state and a
// count; an idle one still marks the card.
func TestAgentMarks(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{
		{PaneID: "p1", Name: "jira-abc-1-a", Status: herdr.Working},
		{PaneID: "p2", Name: "jira-abc-1-b", Status: herdr.Blocked},
		{PaneID: "p3", Name: "jira-abc-2-a", Status: herdr.Idle},
	}})
	m = out.(Model)
	if got := ansi.Strip(m.agentMark("ABC-1")); got != "✋2" {
		t.Errorf("ABC-1 mark %q", got)
	}
	if got := ansi.Strip(m.agentMark("ABC-2")); got != "○" {
		t.Errorf("ABC-2 mark %q", got)
	}
}

// TestAgentAttach: S on an issue with an agent attaches to it instead of
// starting another; the palette lists each agent to attach to.
func TestAgentAttach(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	herdrBin = "true"
	t.Cleanup(func() { herdrBin = "herdr" })
	m := jiraTabModel(t)
	m.herdr = herdr.New("/nowhere.sock")
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{{PaneID: "p1", Name: "jira-abc-1-a", Status: herdr.Idle}}})
	m = out.(Model)
	m.jiraIssue = &jira.Issue{Key: "ABC-1"}
	m.jiraRepos = map[string]string{"ABC": t.TempDir()}
	if cmd := m.startJiraWork(); cmd == nil || !strings.Contains(m.status, "attached") || m.jiraStarting["ABC-1"] {
		t.Fatalf("S should attach: %q", m.status)
	}
	out, _ = m.handleAgentAttached(agentAttachedMsg{key: "ABC-1"})
	if m = out.(Model); !strings.Contains(m.status, "back from") {
		t.Errorf("status %q", m.status)
	}
}

// fakeHerdrCalls serves a herdr socket that answers every request, and
// records each as "method params".
func fakeHerdrCalls(t *testing.T) (*herdr.Client, chan string) {
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	calls := make(chan string, 10)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var req struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			_ = json.Unmarshal(line, &req)
			calls <- req.Method + " " + string(req.Params)
			if req.Method == "tab.create" {
				conn.Write([]byte(`{"id":"x","result":{"tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p9"}}}` + "\n"))
			} else {
				conn.Write([]byte(`{"id":"x","result":{}}` + "\n"))
			}
			conn.Close()
		}
	}()
	return herdr.New(sock), calls
}

// TestAgentPanel: the panel lists the issue's agents; the A menu prompts,
// stops, or starts another in the same worktree.
func TestAgentPanel(t *testing.T) {
	m := jiraTabModel(t)
	c, calls := fakeHerdrCalls(t)
	m.herdr = c
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{{PaneID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1",
		Name: "jira-abc-1-a", CWD: "/wt/abc-1", Title: "Reading the ticket", Status: herdr.Working}}})
	m = out.(Model)
	iss := &jira.Issue{Key: "ABC-1", Summary: "First"}
	m.jiraIssue = iss
	body := ansi.Strip(m.renderJiraIssue(iss, 80))
	if !strings.Contains(body, "Agents (herdr)") || !strings.Contains(body, "⚙ jira-abc-1-a  working  /wt/abc-1") || !strings.Contains(body, "Reading the ticket") {
		t.Fatalf("panel:\n%s", body)
	}
	var ids []string
	for _, it := range m.agentActions("ABC-1") {
		ids = append(ids, it.id)
	}
	if strings.Join(ids, " ") != "agent-attach:w1:p1 agent-prompt:w1:p1 agent-stop:w1:p1 agent-new:w1:p1" {
		t.Fatalf("actions %q", ids)
	}

	m.applyIssueAction("ABC-1", "agent-prompt:w1:p1")
	if m.jiraFieldName != "agent-prompt" || m.agentPane != "w1:p1" {
		t.Fatalf("prompt input: %q %q", m.jiraFieldName, m.agentPane)
	}
	out, cmd := m.applyAgentPrompt("add a test")
	m = out.(Model)
	done := cmd().(agentDoneMsg)
	if got := <-calls; done.err != nil || !strings.HasPrefix(got, "agent.prompt") || !strings.Contains(got, `"text":"add a test"`) {
		t.Fatalf("prompt: %q %v", got, done.err)
	}

	cmd = m.applyIssueAction("ABC-1", "agent-stop:w1:p1")
	if cmd().(agentDoneMsg).err != nil || !strings.Contains(<-calls, `tab.close {"tab_id":"w1:t1"}`) {
		t.Fatal("stop should close the agent's tab")
	}

	m.opts.workAgent = "claude"
	cmd = m.applyIssueAction("ABC-1", "agent-new:w1:p1")
	if cmd().(agentDoneMsg).err != nil {
		t.Fatal("new agent failed")
	}
	if got := <-calls; !strings.HasPrefix(got, "tab.create") || !strings.Contains(got, `"cwd":"/wt/abc-1"`) || !strings.Contains(got, `"workspace_id":"w1"`) {
		t.Errorf("tab: %q", got)
	}
	if got := <-calls; !strings.HasPrefix(got, "agent.start") || !strings.Contains(got, `"pane_id":"w1:p9"`) {
		t.Errorf("start: %q", got)
	}
}

// TestStartWorkAsks: S without an agent asks which kind, ui.work_agent
// first, then the prompt; an empty one starts with the start prompt.
func TestStartWorkAsks(t *testing.T) {
	m := jiraTabModel(t)
	m.herdr = herdr.New("/nowhere.sock")
	m.opts.workAgent = "codex"
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Summary: "First"}
	m.jiraRepos = map[string]string{"ABC": t.TempDir()}
	if cmd := m.startJiraWork(); cmd != nil || m.jiraPicker.kind != jiraPickAgentKind {
		t.Fatalf("S should ask the kind: %v", m.jiraPicker.kind)
	}
	if it := m.jiraPicker.items[m.jiraPicker.idx]; it.id != "codex" {
		t.Fatalf("default first: %+v", it)
	}
	out, _ := m.applyJiraPick()
	m = out.(Model)
	if m.jiraFieldName != "work-prompt" || m.workKind != "codex" || m.jiraFieldKey != "ABC-1" {
		t.Fatalf("prompt input: %q %q %q", m.jiraFieldName, m.workKind, m.jiraFieldKey)
	}
	out, cmd := m.applyWorkPrompt("")
	if m = out.(Model); cmd == nil || !m.jiraStarting["ABC-1"] {
		t.Fatal("should start work")
	}
}
