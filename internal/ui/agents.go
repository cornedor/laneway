package ui

import (
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/rules"
)

// The herdr agents started on issues (S), live on their cards: ⚙ working,
// ✋ waiting on you, ✓ done and not yet seen, ○ idle, with a count when an
// issue has more than one (the worst state shows); ◌ a worktree without
// one. The palette's "worktrees and agents" view lists them all. An agent that starts
// waiting raises a desktop notification. herdr is asked every few seconds;
// while it isn't running, rarely. S on an issue with an agent, a click on
// its card's mark, or enter on its palette row, attaches to its terminal. The panel lists the issue's
// agents; its A menu attaches, prompts or stops one, or starts another in
// the same worktree.

const (
	agentEvery = 5 * time.Second
	agentIdle  = time.Minute // while herdr doesn't answer
)

type agentTickMsg struct{}

type agentsMsg struct {
	agents    []herdr.Agent
	worktrees map[string]string // issue → its linked worktree, from jira.repos
	err       error
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
	var repos []string
	for _, r := range m.jiraRepos {
		repos = append(repos, expandUserPath(r))
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		as, err := c.Agents(ctx)
		if err != nil {
			return agentsMsg{err: err}
		}
		wts := map[string]string{}
		for _, r := range repos {
			for path, branch := range linkedWorktrees(r) {
				if k := branchKey(branch); k != "" {
					wts[k] = path
				}
			}
		}
		return agentsMsg{agents: as, worktrees: wts}
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
		m.agents, m.worktrees = nil, nil
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
	m.agents, m.worktrees = next, msg.worktrees
	m.jiraTab.rows = nil
	m.renderJira()
	return m, tea.Batch(append(cmds, agentTick(agentEvery))...)
}

// agentMark is key's agent state as a card shows it, "" for none.
func (m *Model) agentMark(key string) string {
	as := m.agents[key]
	if len(as) == 0 {
		if m.worktrees[key] != "" {
			return jiraDimStyle.Render("◌")
		}
		return ""
	}
	mark := statusMark(as[0].Status)
	if len(as) > 1 {
		mark += jiraDimStyle.Render(strconv.Itoa(len(as)))
	}
	return mark
}

// herdrBin is the herdr CLI; tests swap it.
var herdrBin = "herdr"

// statusMark is one agent's state as a mark.
func statusMark(s herdr.Status) string {
	switch s {
	case herdr.Working:
		return "⚙"
	case herdr.Blocked:
		return jiraOverStyle.Render("✋")
	case herdr.Done:
		return "✓"
	}
	return jiraDimStyle.Render("○")
}

type agentAttachedMsg struct {
	key string
	err error
}

// attachAgent opens the agent's terminal in pane: inside herdr by focusing
// it, else in the panel (ui.agent_view: panel) or by handing the terminal
// to herdr agent attach until it detaches.
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
	if os.Getenv("HERDR_ENV") != "1" && m.opts.agentView == "panel" {
		return m.openAgentPanel(key, pane, bin)
	}
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

// renderAgents writes the panel's "Agents (herdr)" section: each agent's
// mark, name, state and worktree, and its terminal title under it.
func (m *Model) renderAgents(b *strings.Builder, key string, width int) {
	as := m.agents[key]
	if len(as) == 0 {
		return
	}
	b.WriteString(sectionHead(agentsHead, "  "+strings.Join(firsts(m.agentHints()), " · "), width))
	for _, a := range as {
		b.WriteString(ansi.Truncate(statusMark(a.Status)+" "+a.Name+"  "+string(a.Status)+"  "+refDimStyle.Render(homeShort(a.CWD)), max(width, 1), "…") + "\n")
		if a.Title != "" {
			b.WriteString(refDimStyle.Render(truncate("  "+a.Title, max(width, 1))) + "\n")
		}
	}
}

const agentsHead = "Agents (herdr)"

// agentHints are the Agents heading's keys: attach, and the A menu.
func (m *Model) agentHints() [][2]string {
	return [][2]string{
		{helpKey(m.keys.JiraStart) + " attach", firstKey(m.keys.JiraStart)},
		{helpKey(m.keys.IssueActions) + " more", firstKey(m.keys.IssueActions)},
	}
}

// homeShort writes p under the home directory as ~/….
func homeShort(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(p, home); ok && (rest == "" || rest[0] == '/') {
			return "~" + rest
		}
	}
	return p
}

// agentActions are the A menu's rows for key's agents: attach, prompt and
// stop each, and a new agent in each worktree.
func (m *Model) agentActions(key string) []jiraPickerItem {
	var items []jiraPickerItem
	seen := map[string]bool{}
	for _, a := range m.agents[key] {
		items = append(items,
			jiraPickerItem{id: "agent-attach:" + a.PaneID, label: "Attach to agent " + a.Name + " (" + string(a.Status) + ")"},
			jiraPickerItem{id: "agent-prompt:" + a.PaneID, label: "Send agent " + a.Name + " a prompt"},
			jiraPickerItem{id: "agent-stop:" + a.PaneID, label: "Stop agent " + a.Name + " (closes its tab)"})
		if a.CWD != "" && !seen[a.CWD] {
			seen[a.CWD] = true
			items = append(items, jiraPickerItem{id: "agent-new:" + a.PaneID, label: "New agent in " + homeShort(a.CWD)})
		}
	}
	return items
}

// agentByPane is key's agent in pane.
func (m *Model) agentByPane(key, pane string) (herdr.Agent, bool) {
	i := slices.IndexFunc(m.agents[key], func(a herdr.Agent) bool { return a.PaneID == pane })
	if i < 0 {
		return herdr.Agent{}, false
	}
	return m.agents[key][i], true
}

type agentDoneMsg struct {
	key, what string
	err       error
}

// applyAgentAction runs an A menu agent row: what on the agent in pane.
func (m *Model) applyAgentAction(key, what, pane string) tea.Cmd {
	a, ok := m.agentByPane(key, pane)
	if !ok || m.herdr == nil {
		m.status = key + ": that agent is gone"
		return nil
	}
	c := m.herdr
	switch what {
	case "agent-attach":
		return m.attachAgent(key, pane)
	case "agent-prompt":
		m.openBulkInput("agent-prompt", "what "+a.Name+" should do next")
		m.jiraFieldKey, m.agentPane = key, pane
		return nil
	case "agent-stop":
		m.status = key + ": stopping " + a.Name + "…"
		return agentCall(key, "stopped "+a.Name, func(ctx context.Context) error { return c.CloseTab(ctx, a.TabID) })
	case "agent-new":
		kind, name := m.opts.workAgent, jiraAgentName(key, time.Now())
		args := workArgs(m.opts.workArgs, m.jiraStartPrompt, key)
		m.status = key + ": starting " + kind + " in " + homeShort(a.CWD) + "…"
		return agentCall(key, kind+" started in "+homeShort(a.CWD), func(ctx context.Context) error {
			_, p, err := c.NewTab(ctx, a.WorkspaceID, key, a.CWD, nil)
			if err != nil {
				return err
			}
			return startAgent(ctx, c, kind, name, p, args)
		})
	}
	return nil
}

// applyAgentPrompt sends the typed prompt to the agent picked in the A menu.
func (m Model) applyAgentPrompt(raw string) (tea.Model, tea.Cmd) {
	key, pane, text := m.jiraFieldKey, m.agentPane, strings.TrimSpace(raw)
	m.closeJiraField()
	if text == "" || m.herdr == nil {
		return m, nil
	}
	c := m.herdr
	m.status = key + ": sending the prompt…"
	return m, agentCall(key, "prompt sent", func(ctx context.Context) error {
		err := c.Prompt(ctx, pane, text)
		if herdr.IsCode(err, "agent_blocked") {
			return errors.New("the agent waits on an approval or question: attach to answer it")
		}
		return err
	})
}

// agentCall runs one herdr call, reported as what on success.
func agentCall(key, what string, call func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return agentDoneMsg{key: key, what: what, err: call(ctx)}
	}
}

func (m Model) handleAgentDone(msg agentDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.key + ": " + msg.err.Error())
	} else {
		m.status = msg.key + ": " + msg.what
	}
	return m, m.fetchAgents()
}

// openWorkView shows every issue with an agent or a worktree as a view,
// across projects.
func (m *Model) openWorkView() tea.Cmd {
	keys := slices.Collect(maps.Keys(m.agents))
	for k := range m.worktrees {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		m.status = "no issue has a herdr agent or a worktree"
		return nil
	}
	slices.Sort(keys)
	return m.runNamedJQLView("Work: agents and worktrees", "key in ("+strings.Join(keys, ", ")+") ORDER BY updated DESC")
}

// frameCell is the glyph drawn at screen cell (x, y), a wide one from
// either of its cells; a digit right after a mark counts as the mark.
func (m *Model) frameCell(x, y int) string {
	lines := strings.Split(m.View().Content, "\n")
	if y < 0 || y >= len(lines) || x < 0 {
		return ""
	}
	cell := func(x int) string { return ansi.Strip(ansi.Cut(lines[y], x, x+1)) }
	c := cell(x)
	if c == "" { // the left half of a wide glyph: Cut yields it at its right
		c = cell(x + 1)
	}
	if len(c) == 1 && c[0] >= '0' && c[0] <= '9' && x > 0 {
		if p := cell(x - 1); isAgentGlyph(p) {
			return p
		}
	}
	return c
}

// isAgentGlyph is whether s is one of agentMark's signs.
func isAgentGlyph(s string) bool {
	switch s {
	case "⚙", "✋", "✓", "○":
		return true
	}
	return false
}
