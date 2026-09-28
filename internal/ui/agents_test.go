package ui

import (
	"errors"
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
