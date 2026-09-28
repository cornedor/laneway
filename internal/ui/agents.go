package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/rules"
)

// The herdr agents started on issues (S), live on their cards: ⚙ working,
// ✋ waiting on you, ✓ done and not yet seen, ○ idle, with a count when an
// issue has more than one (the worst state shows). An agent that starts
// waiting raises a desktop notification. herdr is asked every few seconds;
// while it isn't running, rarely. S on an issue with an agent, or enter on
// its palette row, attaches to its terminal.

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

// agentRank orders states worst first: the one that needs you leads.
func agentRank(s herdr.Status) int {
	switch s {
	case herdr.Blocked:
		return 0
	case herdr.Working:
		return 1
	case herdr.Done:
		return 2
	}
	return 3
}

func (m Model) handleAgents(msg agentsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.agents = nil
		return m, agentTick(agentIdle)
	}
	next := map[string][]herdr.Agent{}
	var cmds []tea.Cmd
	for _, a := range msg.agents {
		k := agentKey(a)
		if k == "" {
			continue
		}
		next[k] = append(next[k], a)
		if a.Status == herdr.Blocked && m.agents != nil && !slices.ContainsFunc(m.agents[k], func(o herdr.Agent) bool {
			return o.PaneID == a.PaneID && o.Status == herdr.Blocked
		}) {
			cmds = append(cmds, tea.Raw(rules.NotifySeq(k+": the agent waits on you", a.Title)))
		}
	}
	for _, as := range next {
		slices.SortStableFunc(as, func(a, b herdr.Agent) int { return agentRank(a.Status) - agentRank(b.Status) })
	}
	m.agents = next
	m.jiraTab.rows = nil
	m.renderJira()
	return m, tea.Batch(append(cmds, agentTick(agentEvery))...)
}

// agentMark is key's agent state as a card shows it, "" for none.
func (m *Model) agentMark(key string) string {
	as := m.agents[key]
	if len(as) == 0 {
		return ""
	}
	var mark string
	switch as[0].Status {
	case herdr.Working:
		mark = "⚙"
	case herdr.Blocked:
		mark = jiraOverStyle.Render("✋")
	case herdr.Done:
		mark = "✓"
	default:
		mark = jiraDimStyle.Render("○")
	}
	if len(as) > 1 {
		mark += jiraDimStyle.Render(strconv.Itoa(len(as)))
	}
	return mark
}

// herdrBin is the herdr CLI; tests swap it.
var herdrBin = "herdr"

type agentAttachedMsg struct {
	key string
	err error
}

// attachAgent opens the agent's terminal in pane: inside herdr by focusing
// it, else by handing the terminal to herdr agent attach until it detaches.
func (m *Model) attachAgent(key, pane string) tea.Cmd {
	if m.herdr == nil {
		m.status = "attach needs herdr running"
		return nil
	}
	bin, err := exec.LookPath(herdrBin)
	if err != nil {
		m.fail("attach: no herdr on PATH")
		return nil
	}
	done := func(err error) tea.Msg { return agentAttachedMsg{key: key, err: err} }
	if os.Getenv("HERDR_ENV") == "1" {
		cmd := herdrCommand(bin, m.herdr.Path(), "agent", "focus", pane)
		m.status = key + ": focusing its agent"
		return func() tea.Msg { return done(cmd.Run()) }
	}
	m.status = key + ": attached to its agent"
	return tea.ExecProcess(herdrCommand(bin, m.herdr.Path(), "agent", "attach", pane), done)
}

// herdrCommand runs the herdr CLI against the socket laneway talks to.
func herdrCommand(bin, socket string, args ...string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+socket)
	return cmd
}

func (m Model) handleAgentAttached(msg agentAttachedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.key + ": attach: " + msg.err.Error())
	} else {
		m.status = msg.key + ": back from its agent"
	}
	return m, m.fetchAgents()
}
