package ui

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// TestAgentPanelLive: an agent started on the open issue shows in its panel
// without reopening it.
func TestAgentPanelLive(t *testing.T) {
	m := panelModel(t)
	if strings.Contains(strings.Join(m.panelPlain, "\n"), agentsHead) {
		t.Fatal("no agent yet")
	}
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{{PaneID: "w1:p1", Name: "jira-abc-1-a", Status: herdr.Working}}})
	m = out.(Model)
	if !strings.Contains(strings.Join(m.panelPlain, "\n"), agentsHead) {
		t.Fatalf("panel:\n%s", strings.Join(m.panelPlain, "\n"))
	}
}

// TestStartWorkForm: S opens one form with the agent (ui.work_agent
// first), the branch and the start prompt filled in; the agent is picked
// there, the prompt edited, and the button starts it.
func TestStartWorkForm(t *testing.T) {
	m := jiraTabModel(t)
	m.herdr = herdr.New("/nowhere.sock")
	m.opts.workAgent = "codex"
	m.jiraStartPrompt = "Work on {key}."
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Summary: "First step", Type: "Story"}
	m.jiraRepos = map[string]string{"ABC": t.TempDir()}
	if cmd := m.startJiraWork(); cmd != nil || m.jiraForm == nil || !m.jiraForm.work {
		t.Fatal("S should open the start work form")
	}
	f := m.jiraForm
	if got := jiraValueText(f.fields[0].val); got != "codex" || f.fields[1].val.Text != "issue/ABC-1-first-step" || f.fields[2].val.Text != "Work on ABC-1." {
		t.Fatalf("rows: %q %q %q", got, f.fields[1].val.Text, f.fields[2].val.Text)
	}
	if f.idx != len(f.fields) {
		t.Errorf("cursor on %d, want the button", f.idx)
	}
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"Start work on ABC-1", "Agent", "Branch", "Prompt", "[ Start work ]"} {
		if !strings.Contains(screen, want) {
			t.Errorf("no %q:\n%s", want, screen)
		}
	}
	f.idx = 2
	f.fields[2].val.Text = "Only look."
	out, cmd := m.handleJiraFormKey(keyStr("ctrl+s"))
	if m = out.(Model); cmd == nil || !m.jiraStarting["ABC-1"] || m.jiraForm != nil {
		t.Fatal("ctrl+s should start work")
	}
}

// TestAgentsScreen: ctrl+g lists each agent by state, waiting on you
// first, with the issue's summary and status; tab adds the worktrees
// without one. d twice stops the cursor's agent; esc goes back.
func TestAgentsScreen(t *testing.T) {
	m := jiraTabModel(t)
	if m.openAgents() != nil || m.jiraTab.agentsView != nil {
		t.Fatal("nothing to show")
	}
	c, calls := fakeHerdrCalls(t)
	m.herdr = c
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{
		{PaneID: "w1:p1", TabID: "w1:t1", Name: "jira-abc-2-a", Agent: "claude", Status: herdr.Working, Title: "Writing tests"},
		{PaneID: "w1:p2", TabID: "w1:t2", Name: "jira-abc-3-a", Agent: "claude", Status: herdr.Blocked},
	}, worktrees: map[string]string{"ABC-1": "/wt/abc-1", "ABC-2": "/wt/abc-2"}})
	m = out.(Model)
	if got := ansi.Strip(m.agentMark("ABC-1")); got != "◌" {
		t.Errorf("worktree mark %q", got)
	}
	if head := ansi.Strip(m.View().Content); !strings.Contains(head, "✋1 ⚙1 ctrl+g") {
		t.Errorf("header lacks the agents badge:\n%s", head)
	}
	out, _ = m.handleKey(keyStr("ctrl+g"))
	m = out.(Model)
	s := m.jiraTab.agentsView
	if s == nil {
		t.Fatal("ctrl+g should open the agents")
	}
	rowKeys := func() string {
		var keys []string
		for _, r := range s.rows {
			keys = append(keys, r.key)
		}
		return strings.Join(keys, " ")
	}
	if got := rowKeys(); got != "ABC-3 ABC-2" {
		t.Fatalf("rows %q", got)
	}
	out, _ = m.handleKey(keyStr("tab"))
	if m = out.(Model); rowKeys() != "ABC-3 ABC-2 ABC-1" {
		t.Fatalf("tab: rows %q", rowKeys())
	}
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"Waiting on you", "✋ ABC-3 Third", "Working", "ABC-2 Second", "claude · Writing tests",
		"Worktrees without an agent", "/wt/abc-1", "✋ waiting on you  New · unassigned", "attaching…"} {
		if !strings.Contains(screen, want) {
			t.Errorf("no %q:\n%s", want, screen)
		}
	}

	out, cmd := m.handleKey(keyStr("d"))
	if m = out.(Model); cmd != nil || !strings.Contains(m.status, "again") {
		t.Fatalf("first d should ask: %q", m.status)
	}
	out, cmd = m.handleKey(keyStr("d"))
	m = out.(Model)
	if cmd().(agentDoneMsg).err != nil || !strings.Contains(<-calls, `tab.close {"tab_id":"w1:t2"}`) {
		t.Fatal("d twice should close the agent's tab")
	}

	out, _ = m.handleKey(keyStr("esc"))
	if m = out.(Model); m.jiraTab.agentsView != nil {
		t.Fatal("esc should go back to the board")
	}
}

// TestAgentIssuesLookup: an issue off the board is looked up on each site,
// the shown one first; a key no site knows says so.
func TestAgentIssuesLookup(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{{PaneID: "p1", Name: "jira-xyz-9-a", Status: herdr.Idle}}})
	m = out.(Model)
	m.openAgents()
	out, _ = m.handleAgentIssues(agentIssuesMsg{issues: map[string]agentIssue{"XYZ-9": {}}})
	m = out.(Model)
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "XYZ-9 not found on any site") {
		t.Errorf("screen:\n%s", screen)
	}
	m.jiraTab.agentsView.issues["XYZ-9"] = agentIssue{card: jira.Card{Key: "XYZ-9", Summary: "Elsewhere", Status: "Doing"}, site: "other", found: true}
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "XYZ-9 [other] Elsewhere") || !strings.Contains(screen, "Doing · unassigned · on other") {
		t.Errorf("screen:\n%s", screen)
	}
}

// TestAgentMarkClick: a click on a card's agent mark (or its count)
// attaches to the agent; a click elsewhere on the card only selects it.
func TestAgentMarkClick(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	herdrBin = "true"
	t.Cleanup(func() { herdrBin = "herdr" })
	for _, tc := range []struct {
		name   string
		agents []herdr.Agent
		mark   string
	}{
		{"working", []herdr.Agent{{PaneID: "p1", Name: "jira-abc-3-a", Status: herdr.Working}}, "⚙"},
		{"wide and counted", []herdr.Agent{{PaneID: "p1", Name: "jira-abc-3-a", Status: herdr.Blocked}, {PaneID: "p2", Name: "jira-abc-3-b", Status: herdr.Idle}}, "✋2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := jiraTabModel(t)
			m.width, m.height = 120, 30
			m.resize()
			m.herdr = herdr.New("/nowhere.sock")
			out, _ := m.handleAgents(agentsMsg{agents: tc.agents})
			m = out.(Model)
			for i, row := range strings.Split(m.View().Content, "\n") {
				if w := ansi.StringWidth(row); w != m.width {
					t.Errorf("row %d is %d wide, want %d", i, w, m.width)
				}
			}
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			y, x := -1, -1
			for i, l := range lines {
				if j := strings.Index(l, tc.mark+" ABC-3"); j >= 0 {
					y, x = i, ansi.StringWidth(l[:j])
				}
			}
			if y < 0 {
				t.Fatalf("no mark on the card:\n%s", strings.Join(lines, "\n"))
			}
			for dx := 0; dx < ansi.StringWidth(tc.mark); dx++ {
				out, cmd := m.handleClick(tea.MouseClickMsg{X: x + dx, Y: y, Button: tea.MouseLeft})
				if mm := out.(Model); cmd == nil || !strings.Contains(mm.status, "ABC-3: attached") {
					t.Fatalf("click at +%d should attach: %q", dx, mm.status)
				}
			}
			out, cmd := m.handleClick(tea.MouseClickMsg{X: x + ansi.StringWidth(tc.mark) + 2, Y: y, Button: tea.MouseLeft})
			if mm := out.(Model); cmd != nil || strings.Contains(mm.status, "attached") {
				t.Fatalf("the title only selects: %q", mm.status)
			}
		})
	}
}
