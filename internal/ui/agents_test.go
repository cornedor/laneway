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
	if cmd == nil || m.agents["ABC-1"] != herdr.Working || m.agents["ABC-2"] != herdr.Done {
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
