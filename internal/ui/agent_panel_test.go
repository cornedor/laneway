//go:build !windows

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
)

func TestEncodeKey(t *testing.T) {
	cases := []struct {
		k    tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: 'a', Text: "a"}, "a"},
		{tea.KeyPressMsg{Code: 'a', Text: "A", Mod: tea.ModShift}, "A"},
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "\x03"},
		{tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}, "\x1bb"},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, "\r"},
		{tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, "\x1b\r"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "\x1b[Z"},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, "\x1b"},
		{tea.KeyPressMsg{Code: tea.KeyBackspace}, "\x7f"},
		{tea.KeyPressMsg{Code: tea.KeyUp}, "\x1b[A"},
		{tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl}, "\x1b[1;5D"},
		{tea.KeyPressMsg{Code: tea.KeyDelete}, "\x1b[3~"},
		{tea.KeyPressMsg{Code: tea.KeyF1}, "\x1bOP"},
		{tea.KeyPressMsg{Code: tea.KeyF5}, "\x1b[15~"},
		{tea.KeyPressMsg{Code: '_', Mod: tea.ModCtrl}, "\x1f"},
	}
	for _, c := range cases {
		if got := encodeKey(c.k); got != c.want {
			t.Errorf("%s: got %q, want %q", c.k.String(), got, c.want)
		}
	}
}

// waitScreen polls the session's screen until it contains want.
func waitScreen(t *testing.T, s *termSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(ansi.Strip(s.view()), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, ansi.Strip(s.view()))
}

// fakeHerdrBin is a herdr CLI whose agent attach prints the pane and echoes
// what it's typed, until it reads "quit"; any other pane fails.
func fakeHerdrBin(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
[ "$3" = "p1" ] || { echo "no agent $3"; exit 1; }
echo "attached $3 via $HERDR_SOCKET_PATH"
while read line; do [ "$line" = quit ] && exit 0; echo "got $line"; done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	herdrBin = bin
	t.Cleanup(func() { herdrBin = "herdr" })
}

func agentPanelModel(t *testing.T) Model {
	t.Setenv("HERDR_ENV", "")
	fakeHerdrBin(t)
	m := jiraTabModel(t)
	m.width, m.height = 120, 30
	m.resize()
	m.opts.agentView = "panel"
	m.herdr = herdr.New("/fake.sock")
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{{PaneID: "p1", Name: "jira-abc-1-a", Status: herdr.Idle}}})
	m = out.(Model)
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Summary: "First", Status: "To Do"}
	m.refOpen, m.focus = true, focusRef
	m.resize()
	return m
}

// TestAgentPanelAttach: with agent_view panel, S attaches in the panel under the
// issue's strip; keys go to the agent, ctrl+\ goes back to the issue.
func TestAgentPanelAttach(t *testing.T) {
	m := agentPanelModel(t)
	if cmd := m.startJiraWork(); cmd == nil || m.agentTerm == nil {
		t.Fatalf("S should attach in the panel: %q", m.status)
	}
	s := m.agentTerm
	defer s.stop()
	waitScreen(t, s, "attached p1 via /fake.sock")
	for _, k := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 's', Text: "s"}, {Code: tea.KeyEnter}} {
		out, _ := m.handleKey(k)
		m = out.(Model)
	}
	waitScreen(t, s, "got qs")
	if !m.agentTermShown() || m.focus != focusRef {
		t.Fatal("q and s must reach the agent, not laneway")
	}
	out, _ := m.update(tea.PasteMsg{Content: "pasted\n"})
	m = out.(Model)
	waitScreen(t, s, "got pasted")

	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "↰ ABC-1") || !strings.Contains(frame, "Agent  jira-abc-1-a") || !strings.Contains(frame, "got qs") {
		t.Fatalf("panel:\n%s", frame)
	}
	for i, row := range strings.Split(m.View().Content, "\n") {
		if w := lipgloss.Width(row); w != m.width {
			t.Errorf("row %d is %d wide, want %d", i, w, m.width)
		}
	}

	out, _ = m.handleKey(tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl})
	m = out.(Model)
	if m.agentTerm != nil || !strings.Contains(m.status, "back from") {
		t.Fatalf("ctrl+\\ should leave: %q", m.status)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the attach did not end")
	}
}

// TestAgentPanelStrip: a click on the issue's strip goes back to it; the
// attach ending on its own does too, and a failed one says why.
func TestAgentPanelStrip(t *testing.T) {
	m := agentPanelModel(t)
	m.startJiraWork()
	waitScreen(t, m.agentTerm, "attached")
	listW, _ := m.jiraListWidth(m.width)
	out, _ := m.handleClick(tea.MouseClickMsg{X: listW + 5, Y: 1, Button: tea.MouseLeft})
	if m = out.(Model); m.agentTerm != nil {
		t.Fatal("a click on the strip should go back to the issue")
	}

	m.startJiraWork()
	s := m.agentTerm
	waitScreen(t, s, "attached")
	s.write("quit\r")
	<-s.done
	out, _ = m.handleTermOutput(s, true)
	if m = out.(Model); m.agentTerm != nil || m.statusIsErr() {
		t.Fatalf("detached: %q", m.status)
	}

	m.agents["ABC-1"][0].PaneID = "p2"
	m.startJiraWork()
	s = m.agentTerm
	<-s.done
	out, _ = m.handleTermOutput(s, true)
	if m = out.(Model); !m.statusIsErr() || !strings.Contains(m.status, "no agent p2") {
		t.Fatalf("failed attach: %q", m.status)
	}
}

// TestAgentPanelCloses: showing another issue detaches the panel's agent.
func TestAgentPanelCloses(t *testing.T) {
	m := agentPanelModel(t)
	m.startJiraWork()
	s := m.agentTerm
	out, _ := m.showJiraKey("ABC-2")
	if m = out.(Model); m.agentTerm != nil {
		t.Fatal("another issue should close the terminal")
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the attach did not end")
	}
}

// TestAgentsScreenTerm: the agents screen attaches to the cursor's agent
// once it rests there and shows its terminal beside the list; enter types
// into it (":" too, not the palette), agent_back goes back to the list,
// and moving to a row without an agent detaches.
func TestAgentsScreenTerm(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	fakeHerdrBin(t)
	m := jiraTabModel(t)
	m.herdr = herdr.New("/h.sock")
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{{PaneID: "p1", Name: "jira-abc-2-a", Status: herdr.Blocked}},
		worktrees: map[string]string{"ABC-1": "/wt/abc-1"}})
	m = out.(Model)
	m.openAgents()
	s := m.jiraTab.agentsView
	out, _ = m.handleAgentTermDue(agentTermDueMsg{seq: s.termSeq})
	m = out.(Model)
	if !m.agentsTermShown() || m.agentTermShown() {
		t.Fatal("the terminal should show on the screen, not the panel")
	}
	waitScreen(t, m.agentTerm, "attached p1 via /h.sock")
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "attached p1 via /h.sock") || !strings.Contains(screen, "enter or a click to type") {
		t.Fatalf("screen:\n%s", screen)
	}
	out, _ = m.handleKey(keyStr("enter"))
	m = out.(Model)
	for _, k := range []string{":", "h", "i", "enter"} {
		out, _ = m.handleKey(keyStr(k))
		m = out.(Model)
	}
	if m.jiraPicker.active {
		t.Fatal(": should reach the agent, not open the palette")
	}
	waitScreen(t, m.agentTerm, "got :hi")
	out, _ = m.handleKey(keyStr("ctrl+\\"))
	if m = out.(Model); m.jiraTab.agentsView.typing {
		t.Fatal("agent_back should go back to the list")
	}
	out, _ = m.handleKey(keyStr("tab"))
	m = out.(Model)
	out, _ = m.handleKey(keyStr("j"))
	if m = out.(Model); m.agentTerm != nil {
		t.Fatal("a row without an agent should detach")
	}
}
