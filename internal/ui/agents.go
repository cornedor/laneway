package ui

import (
	"context"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/rules"
)

// The herdr agents started on issues (S), live on their cards: ⚙ working,
// ✋ waiting on you, ✓ done and not yet seen. An agent that starts waiting
// raises a desktop notification. herdr is asked every few seconds; while
// it isn't running, rarely.

const (
	agentEvery = 5 * time.Second
	agentIdle  = time.Minute // while herdr doesn't answer
)

type agentTickMsg struct{}

type agentsMsg struct {
	agents []herdr.Agent
	err    error
}

func agentTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return agentTickMsg{} })
}

// fetchAgents asks herdr for its agents.
func (m *Model) fetchAgents() tea.Cmd {
	c := m.herdr
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		as, err := c.Agents(ctx)
		return agentsMsg{as, err}
	}
}

// agentKey is the issue an agent works on: from its name (jira-abc-12-…)
// or its worktree's directory, "" for none.
func agentKey(a herdr.Agent) string {
	if k := branchKey(a.Name); k != "" {
		return k
	}
	return branchKey(filepath.Base(a.CWD))
}

func (m Model) handleAgents(msg agentsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.agents = nil
		return m, agentTick(agentIdle)
	}
	next := map[string]herdr.Status{}
	var cmds []tea.Cmd
	for _, a := range msg.agents {
		k := agentKey(a)
		if k == "" {
			continue
		}
		next[k] = a.Status
		if a.Status == herdr.Blocked && m.agents[k] != herdr.Blocked && m.agents != nil {
			cmds = append(cmds, tea.Raw(rules.NotifySeq(k+": the agent waits on you", a.Title)))
		}
	}
	m.agents = next
	m.jiraTab.rows = nil
	m.renderJira()
	return m, tea.Batch(append(cmds, agentTick(agentEvery))...)
}

// agentMark is key's agent state as a card shows it, "" for none.
func (m *Model) agentMark(key string) string {
	switch m.agents[key] {
	case herdr.Working:
		return "⚙"
	case herdr.Blocked:
		return jiraOverStyle.Render("✋")
	case herdr.Done:
		return "✓"
	}
	return ""
}
