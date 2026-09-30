package ui

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/safeterm"
)

// ctrl+g swaps the board for the agents: every herdr agent on an issue, a
// row each, grouped by state (waiting on you first); tab adds the
// worktrees without one. Beside the list, when wide enough, the cursor's issue, its
// agent and what its terminal shows now, read again on every agent poll.
// An issue on another configured site says which; the lookup tries the
// shown site first.

type agentsScreen struct {
	rows    []agentRow
	row     int
	top     int                   // the first list line shown
	issues  map[string]agentIssue // by key; looked up once a screen
	asking  bool
	bare    bool              // the worktrees without an agent show too
	screens map[string]string // pane → its terminal's text
	stopAsk string            // the pane a first stop press was for
	listW   int               // as last drawn, for a click
	lineRow []int             // each list line's row, -1 for none
}

// agentRow is one agent on key, or key's worktree when it has none.
type agentRow struct {
	key   string
	agent herdr.Agent // PaneID "" for a worktree alone
	path  string
}

// agentIssue is key's card and site; found is false when no site knows it.
type agentIssue struct {
	card  jira.Card
	site  string
	url   string
	found bool
}

type agentIssuesMsg struct{ issues map[string]agentIssue }

type agentScreenMsg struct{ pane, text string }

// agentGroups name the list's groups, by agentGroup.
var agentGroups = []string{"Waiting on you", "Working", "Done", "Idle", "Worktrees without an agent"}

func agentGroup(r agentRow) int {
	if r.agent.PaneID == "" {
		return 4
	}
	return agentRank(r.agent.Status)
}

// agentState is how a row names its agent's state.
func agentState(s herdr.Status) string {
	switch s {
	case herdr.Blocked:
		return "waiting on you"
	case herdr.Working:
		return "working"
	case herdr.Done:
		return "done"
	case herdr.Idle:
		return "idle"
	}
	return "unknown"
}

// openAgents swaps the board for the agents screen.
func (m *Model) openAgents() tea.Cmd {
	if len(m.agents) == 0 && len(m.worktrees) == 0 {
		if m.herdr == nil {
			m.status = "no herdr running"
		} else {
			m.status = "no issue has a herdr agent or a worktree · " + helpKey(m.keys.JiraStart) + " starts one"
		}
		return nil
	}
	s := &agentsScreen{issues: map[string]agentIssue{}, screens: map[string]string{}, bare: len(m.agents) == 0}
	m.jiraTab.agentsView = s
	m.focus = focusJira
	for _, c := range m.jiraTab.cards {
		s.issues[c.Key] = agentIssue{card: c, site: m.site, url: m.jiraClient.BrowseURL(c.Key), found: true}
	}
	m.buildAgentRows()
	return tea.Batch(m.lookUpAgentIssues(), m.readAgentScreen())
}

// buildAgentRows lists the agents by group, then key; the cursor stays on
// its agent, or its issue.
func (m *Model) buildAgentRows() {
	s := m.jiraTab.agentsView
	keep := s.cursor()
	var rows []agentRow
	for k, as := range m.agents {
		for _, a := range as {
			rows = append(rows, agentRow{key: k, agent: a, path: a.CWD})
		}
	}
	for k, p := range m.worktrees {
		if s.bare && len(m.agents[k]) == 0 {
			rows = append(rows, agentRow{key: k, path: p})
		}
	}
	slices.SortStableFunc(rows, func(a, b agentRow) int {
		return cmp.Or(cmp.Compare(agentGroup(a), agentGroup(b)), cmp.Compare(a.key, b.key), cmp.Compare(a.agent.Name, b.agent.Name))
	})
	s.rows = rows
	i := slices.IndexFunc(rows, func(r agentRow) bool { return keep.agent.PaneID != "" && r.agent.PaneID == keep.agent.PaneID })
	if i < 0 {
		i = slices.IndexFunc(rows, func(r agentRow) bool { return r.key == keep.key })
	}
	if i >= 0 {
		s.row = i
	}
	s.row = min(s.row, max(len(rows)-1, 0))
}

func (s *agentsScreen) cursor() agentRow {
	if s.row < len(s.rows) {
		return s.rows[s.row]
	}
	return agentRow{}
}

// lookUpAgentIssues reads the cards of the rows' issues not yet looked up,
// on every site, one key at a time: a search naming a key a site lacks
// fails as a whole.
func (m *Model) lookUpAgentIssues() tea.Cmd {
	s := m.jiraTab.agentsView
	var keys []string
	for _, r := range s.rows {
		if _, ok := s.issues[r.key]; !ok && !slices.Contains(keys, r.key) {
			keys = append(keys, r.key)
		}
	}
	if len(keys) == 0 || s.asking {
		return nil
	}
	s.asking = true
	sites := map[string]*jira.Client{}
	if m.jiraClient.Enabled() {
		sites[m.site] = m.jiraClient
	}
	maps.Copy(sites, m.others())
	order := slices.Sorted(maps.Keys(sites))
	if i := slices.Index(order, m.site); i > 0 {
		order = append([]string{m.site}, slices.Delete(order, i, i+1)...)
	}
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		got := make([][]agentIssue, len(keys))
		var wg sync.WaitGroup
		for i, k := range keys {
			got[i] = make([]agentIssue, len(order))
			for j, site := range order {
				c := sites[site]
				wg.Go(func() {
					if cards, err := c.SearchCards(ctx, "key = "+k); err == nil && len(cards) == 1 {
						got[i][j] = agentIssue{card: cards[0], site: site, url: c.BrowseURL(k), found: true}
					}
				})
			}
		}
		wg.Wait()
		out := map[string]agentIssue{}
		for i, k := range keys {
			out[k] = agentIssue{}
			for _, is := range got[i] {
				if is.found {
					out[k] = is
					break
				}
			}
		}
		return agentIssuesMsg{issues: out}
	}
}

func (m Model) handleAgentIssues(msg agentIssuesMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.agentsView
	if s == nil {
		return m, nil
	}
	s.asking = false
	maps.Copy(s.issues, msg.issues)
	return m, m.lookUpAgentIssues() // keys that showed up meanwhile
}

// readAgentScreen reads what the cursor's agent's terminal shows.
func (m *Model) readAgentScreen() tea.Cmd {
	s, c := m.jiraTab.agentsView, m.herdr
	pane := s.cursor().agent.PaneID
	if c == nil || pane == "" || !m.agentsSplit() {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		text, err := c.Read(ctx, pane)
		if err != nil {
			return nil
		}
		return agentScreenMsg{pane: pane, text: text}
	}
}

func (m Model) handleAgentScreen(msg agentScreenMsg) (tea.Model, tea.Cmd) {
	if s := m.jiraTab.agentsView; s != nil {
		s.screens[msg.pane] = msg.text
	}
	return m, nil
}

// agentsRefreshed rebuilds the open screen after an agent poll.
func (m *Model) agentsRefreshed() tea.Cmd {
	if m.jiraTab.agentsView == nil {
		return nil
	}
	m.buildAgentRows()
	return tea.Batch(m.lookUpAgentIssues(), m.readAgentScreen())
}

// agentsSplit is whether the detail shows beside the list.
func (m *Model) agentsSplit() bool { return m.jiraTab.view.Width() >= inboxSplitMin }

func (m *Model) moveAgents(d int) tea.Cmd {
	s := m.jiraTab.agentsView
	s.row = min(max(s.row+d, 0), max(len(s.rows)-1, 0))
	s.stopAsk = ""
	return m.readAgentScreen()
}

func (m Model) handleAgentsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s, k := m.jiraTab.agentsView, m.keys
	r := s.cursor()
	is := s.issues[r.key]
	here := !is.found || is.site == m.site
	needAgent := func() bool {
		if r.agent.PaneID == "" {
			m.status = r.key + " has no agent here, only its worktree"
		}
		return r.agent.PaneID != ""
	}
	if !key.Matches(msg, k.AgentStop) {
		s.stopAsk = ""
	}
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, k.Quit), key.Matches(msg, k.Agents):
		m.jiraTab.agentsView = nil
		m.renderJira()
	case key.Matches(msg, k.Tab), key.Matches(msg, k.ShiftTab):
		s.bare = !s.bare
		m.buildAgentRows()
		return m, tea.Batch(m.lookUpAgentIssues(), m.readAgentScreen())
	case r.key == "":
	case key.Matches(msg, k.Up):
		return m, m.moveAgents(-1)
	case key.Matches(msg, k.Down):
		return m, m.moveAgents(1)
	case key.Matches(msg, k.Home):
		return m, m.moveAgents(-len(s.rows))
	case key.Matches(msg, k.End):
		return m, m.moveAgents(len(s.rows))
	case key.Matches(msg, k.OpenChannel):
		if r.agent.PaneID != "" {
			return m, m.attachAgentIn(r.key, r.agent.PaneID, here)
		}
		fallthrough
	case key.Matches(msg, k.OpenRef):
		if !here {
			m.status = "opening " + is.url + "…"
			return m, m.openOpenable(openable{name: r.key, url: is.url})
		}
		return m.openJiraKey(r.key)
	case key.Matches(msg, k.OpenAttach):
		url := is.url
		if url == "" {
			url = m.jiraClient.BrowseURL(r.key)
		}
		m.status = "opening " + url + "…"
		return m, m.openOpenable(openable{name: r.key, url: url})
	case key.Matches(msg, k.AgentPrompt):
		if needAgent() {
			return m, m.applyAgentAction(r.key, "agent-prompt", r.agent.PaneID)
		}
	case key.Matches(msg, k.AgentStop):
		if !needAgent() {
			break
		}
		if s.stopAsk != r.agent.PaneID {
			s.stopAsk = r.agent.PaneID
			m.status = "stop " + r.agent.Name + " and close its tab? " + helpKey(k.AgentStop) + " again"
			break
		}
		s.stopAsk = ""
		return m, m.applyAgentAction(r.key, "agent-stop", r.agent.PaneID)
	case key.Matches(msg, k.CopyKey):
		m.status = r.key + " copied"
		return m, tea.SetClipboard(r.key)
	case key.Matches(msg, k.Refresh):
		clear(s.issues)
		return m, tea.Batch(m.fetchAgents(), m.lookUpAgentIssues())
	case key.Matches(msg, k.Help):
		m.openHelp("Agents")
	}
	return m, nil
}

// clickAgents puts the cursor on the row clicked; a double click attaches.
func (m Model) clickAgents(x, y, count int) (tea.Model, tea.Cmd) {
	s := m.jiraTab.agentsView
	i := s.rowAt(x, y)
	if i < 0 {
		return m, nil
	}
	m.focus = focusJira
	s.row = i
	cmd := m.readAgentScreen()
	if count >= 2 {
		out, c := m.handleAgentsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		return out, tea.Batch(cmd, c)
	}
	return m, cmd
}

// rowAt is the row drawn at screen cell x, y, -1 for none.
func (s *agentsScreen) rowAt(x, y int) int {
	line := s.top + y - jiraBodyTop
	if y < jiraBodyTop || x > s.listW+1 || line >= len(s.lineRow) {
		return -1
	}
	return s.lineRow[line]
}

// agentsViewLine is the view line: the count of each state and the keys.
func (m *Model) agentsViewLine() string {
	s, k := m.jiraTab.agentsView, m.keys
	var n [5]int
	for _, as := range m.agents {
		for _, a := range as {
			n[agentRank(a.Status)]++
		}
	}
	for k := range m.worktrees {
		if len(m.agents[k]) == 0 {
			n[4]++
		}
	}
	parts := []string{jiraViewActive.Render("Agents")}
	for i, mark := range []string{"✋", "⚙", "✓", "○", "◌"} {
		if n[i] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", mark, n[i]))
		}
	}
	show := "shows"
	if s.bare {
		show = "hides"
	}
	keys := fmt.Sprintf("  ·  %s %s ◌ · %s attach · %s issue · %s prompt · %s stop · %s browser · esc board",
		helpKey(k.Tab), show, helpKey(k.OpenChannel), helpKey(k.OpenRef), helpKey(k.AgentPrompt), helpKey(k.AgentStop), helpKey(k.OpenAttach))
	return parts[0] + jiraDimStyle.Render("  "+strings.Join(parts[1:], "  ")+keys)
}

// renderAgentsScreen draws the list and, when wide enough, the cursor's detail
// beside it.
func (m *Model) renderAgentsScreen(width, height int) string {
	s := m.jiraTab.agentsView
	if len(s.rows) == 0 {
		s.lineRow = nil
		if !s.bare {
			return "\n  no agents · " + helpKey(m.keys.Tab) + " shows the worktrees without one"
		}
		return "\n  no agents or worktrees left · esc goes back to the board"
	}
	listW := width
	if m.agentsSplit() {
		listW = min(max(width*2/5, 36), 64)
	}
	s.listW = listW
	list := m.renderAgentsList(listW, height)
	if listW == width {
		return strings.Join(list, "\n")
	}
	detail := m.renderAgentDetail(width-listW-3, height)
	out := make([]string, height)
	for i := range out {
		l, r := "", ""
		if i < len(list) {
			l = list[i]
		}
		if i < len(detail) {
			r = detail[i]
		}
		out[i] = pad(l, listW) + refDimStyle.Render(" │ ") + r
	}
	return strings.Join(out, "\n")
}

// renderAgentsList draws each group under its name, two lines a row: the
// mark, key, summary and status; then the agent's name and terminal title,
// or the worktree's path.
func (m *Model) renderAgentsList(width, height int) []string {
	s := m.jiraTab.agentsView
	var out []string
	s.lineRow = s.lineRow[:0]
	add := func(row int, l string) {
		out = append(out, l)
		s.lineRow = append(s.lineRow, row)
	}
	cursorAt := 0
	group := -1
	for i, r := range s.rows {
		if g := agentGroup(r); g != group {
			if group >= 0 {
				add(-1, "")
			}
			group = g
			add(-1, refLabelStyle.Render(agentGroups[g]))
		}
		is, looked := s.issues[r.key]
		summary := is.card.Summary
		switch {
		case !looked:
			summary = "…"
		case !is.found:
			summary = "not found on any site"
		case is.site != m.site:
			summary = "[" + is.site + "] " + summary
		}
		mark := jiraDimStyle.Render("◌")
		if r.agent.PaneID != "" {
			mark = statusMark(r.agent.Status)
		}
		status := is.card.Status
		headW := max(width-2-ansi.StringWidth(status)-1, 8)
		head := ansi.Truncate(jiraKeyStyle.Render(r.key)+" "+summary, headW, "…")
		line1 := mark + " " + pad(head, headW) + " " + refDimStyle.Render(status)

		sub := homeShort(r.path)
		if r.agent.PaneID != "" {
			sub = cmp.Or(r.agent.Agent, r.agent.Name)
			if r.agent.Title != "" {
				sub += " · " + safeterm.Line(r.agent.Title)
			}
		}
		line2 := "  " + ansi.Truncate(sub, max(width-2, 1), "…")
		if i == s.row {
			cursorAt = len(out)
			add(i, selectedRow.Render(pad(ansi.Strip(line1), width)))
			add(i, selectedRow.Render(pad(line2, width)))
			continue
		}
		add(i, line1)
		add(i, refDimStyle.Render(line2))
	}
	if cursorAt < s.top {
		s.top = max(cursorAt-1, 0) // with its group's name when just above
	}
	if cursorAt+2 > s.top+height {
		s.top = cursorAt + 2 - height
	}
	s.top = min(max(s.top, 0), max(len(out)-height, 0))
	s.lineRow = s.lineRow[s.top:min(len(out), s.top+height)]
	return out[s.top:min(len(out), s.top+height)]
}

// renderAgentDetail draws the cursor's issue and agent, then the bottom of
// what its terminal shows.
func (m *Model) renderAgentDetail(width, height int) []string {
	s := m.jiraTab.agentsView
	r := s.cursor()
	if r.key == "" || width < 10 {
		return nil
	}
	is := s.issues[r.key]
	fit := func(l string) string { return ansi.Truncate(l, width, "…") }
	out := []string{fit(jiraKeyStyle.Render(r.key) + " " + titleStyle.Render(is.card.Summary))}
	if is.found {
		facts := []string{is.card.Status, cmp.Or(is.card.Assignee, "unassigned")}
		if is.site != m.site {
			facts = append(facts, "on "+is.site)
		}
		out = append(out, refDimStyle.Render(fit(strings.Join(facts, " · "))))
	}
	out = append(out, "")
	a := r.agent
	if a.PaneID == "" {
		out = append(out, "◌ "+refDimStyle.Render("a worktree, no agent in it"), refDimStyle.Render(fit("  "+homeShort(r.path))))
		return out
	}
	who := cmp.Or(a.Name, a.Agent, a.PaneID)
	if a.Agent != "" && a.Agent != who {
		who += " · " + a.Agent
	}
	out = append(out, fit(statusMark(a.Status)+" "+who+refDimStyle.Render(" · "+agentState(a.Status))),
		refDimStyle.Render(fit("  "+homeShort(a.CWD))))
	if a.Title != "" {
		out = append(out, refDimStyle.Render(fit("  "+safeterm.Line(a.Title))))
	}
	room := height - len(out) - 2
	text, ok := s.screens[a.PaneID]
	if room < 3 {
		return out
	}
	out = append(out, "", refDimStyle.Render(fit("── its terminal "+strings.Repeat("─", max(width-16, 0)))))
	if !ok {
		return append(out, refDimStyle.Render("  reading…"))
	}
	lines := strings.Split(strings.TrimRight(safeterm.Text(text), "\n \t"), "\n")
	lines = lines[max(len(lines)-room, 0):]
	for _, l := range lines {
		out = append(out, fit(strings.TrimRight(l, " \r")))
	}
	return out
}

// agentsBadge is the header's count of the agents waiting on you and
// working, "" for none.
func (m *Model) agentsBadge() string {
	var blocked, working int
	for _, as := range m.agents {
		for _, a := range as {
			switch a.Status {
			case herdr.Blocked:
				blocked++
			case herdr.Working:
				working++
			}
		}
	}
	var parts []string
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("✋%d", blocked))
	}
	if working > 0 {
		parts = append(parts, fmt.Sprintf("⚙%d", working))
	}
	return strings.Join(parts, " ")
}
