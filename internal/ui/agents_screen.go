package ui

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os/exec"
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
// worktrees without one. Beside the list, when wide enough, the cursor's
// agent's own terminal (herdr agent attach, as ui.agent_view: panel runs
// it), attached once the cursor rests on it: enter or a click types into
// it, agent_back (ctrl+\) goes back to the list. An issue on another
// configured site says which; the lookup tries the shown site first.

type agentsScreen struct {
	rows    []agentRow
	row     int
	top     int                   // the first list line shown
	issues  map[string]agentIssue // by key; looked up once a screen
	asking  bool
	bare    bool   // the worktrees without an agent show too
	stopAsk string // the pane a first stop press was for
	termFor string // the pane the terminal is attached to
	typing  bool   // keys go to the terminal
	termSeq int    // the attach the cursor last asked for
	termErr string // why the attach failed
	listW   int    // as last drawn, for a click
	lineRow []int  // each list line's row, -1 for none
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

// agentTermDueMsg attaches to the cursor's agent once it rests there.
type agentTermDueMsg struct{ seq int }

// agentTermWait is how long the cursor rests on a row before its agent is
// attached: holding j does not start one attach a row.
const agentTermWait = 150 * time.Millisecond

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
	s := &agentsScreen{issues: map[string]agentIssue{}, bare: len(m.agents) == 0}
	m.jiraTab.agentsView = s
	m.focus = focusJira
	for _, c := range m.jiraTab.cards {
		s.issues[c.Key] = agentIssue{card: c, site: m.site, url: m.jiraClient.BrowseURL(c.Key), found: true}
	}
	m.buildAgentRows()
	return tea.Batch(m.lookUpAgentIssues(), m.scheduleAgentTerm())
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

// scheduleAgentTerm attaches to the cursor's agent after agentTermWait,
// unless the terminal shows it already; a row without one detaches.
func (m *Model) scheduleAgentTerm() tea.Cmd {
	s := m.jiraTab.agentsView
	pane := s.cursor().agent.PaneID
	if pane != "" && pane == s.termFor && (m.agentsTermShown() || s.termErr != "") {
		return nil
	}
	s.termSeq++
	if pane == "" || !m.agentsSplit() {
		m.closeAgentsTerm()
		return nil
	}
	seq := s.termSeq
	return tea.Tick(agentTermWait, func(time.Time) tea.Msg { return agentTermDueMsg{seq: seq} })
}

func (m Model) handleAgentTermDue(msg agentTermDueMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.agentsView
	if s == nil || msg.seq != s.termSeq {
		return m, nil
	}
	r := s.cursor()
	if r.agent.PaneID == "" || m.herdr == nil {
		return m, nil
	}
	m.closeAgentPanel() // the one terminal, in the panel or here
	s.termFor, s.termErr, s.typing = r.agent.PaneID, "", false
	bin, err := exec.LookPath(herdrBin)
	if err != nil {
		s.termErr = "no herdr on PATH"
		return m, nil
	}
	w, h := m.agentsTermSize()
	t, err := startTerm(termSpec{
		title: cmp.Or(r.agent.Name, r.agent.Agent),
		argv:  []string{bin, "agent", "attach", r.agent.PaneID},
		env:   []string{"HERDR_SOCKET_PATH=" + m.herdr.Path()},
	}, w, h)
	if err != nil {
		s.termErr = err.Error()
		return m, nil
	}
	m.agentTerm, m.agentTermKey, m.agentTermScreen = t, r.key, true
	return m, waitTermOutput(t)
}

// agentsTermShown is whether the agents screen shows the terminal.
func (m *Model) agentsTermShown() bool {
	return m.agentTerm != nil && m.agentTermScreen && m.jiraTab.agentsView != nil
}

// closeAgentsTerm detaches the screen's terminal; the agent keeps running.
func (m *Model) closeAgentsTerm() {
	if m.agentTermScreen {
		m.closeAgentPanel()
	}
	if s := m.jiraTab.agentsView; s != nil {
		s.termFor, s.typing = "", false
	}
}

// agentsTermExited says why the screen's terminal ended; moving off the
// row and back attaches again.
func (m Model) agentsTermExited(t *termSession) (tea.Model, tea.Cmd) {
	why := t.exitStatus() + lastLine(t.view())
	pane := ""
	if s := m.jiraTab.agentsView; s != nil {
		pane = s.termFor
	}
	m.closeAgentsTerm()
	if s := m.jiraTab.agentsView; s != nil {
		s.termFor, s.termErr = pane, "detached · "+why
	}
	return m, m.fetchAgents()
}

// agentsListW is the list's width beside the detail, or all of width.
func (m *Model) agentsListW(width int) int {
	if !m.agentsSplit() {
		return width
	}
	return min(max(width*2/5, 36), 64)
}

// agentsTermHead is the lines above the terminal: the issue, the agent.
const agentsTermHead = 2

// agentsTermSize is the terminal's size: the detail under its head.
func (m *Model) agentsTermSize() (w, h int) {
	v := m.jiraTab.view
	return max(v.Width()-m.agentsListW(v.Width())-3, 10), max(v.Height()-agentsTermHead, 3)
}

// agentsTermOrigin is the screen cell of the terminal's top-left: past the
// box's border, the list and its " │ ".
func (m *Model) agentsTermOrigin() (x, y int) {
	return 1 + m.agentsListW(m.jiraTab.view.Width()) + 3, jiraBodyTop + agentsTermHead
}

// agentsRefreshed rebuilds the open screen after an agent poll.
func (m *Model) agentsRefreshed() tea.Cmd {
	if m.jiraTab.agentsView == nil {
		return nil
	}
	m.buildAgentRows()
	return tea.Batch(m.lookUpAgentIssues(), m.scheduleAgentTerm())
}

// agentsSplit is whether the detail shows beside the list.
func (m *Model) agentsSplit() bool { return m.jiraTab.view.Width() >= inboxSplitMin }

func (m *Model) moveAgents(d int) tea.Cmd {
	s := m.jiraTab.agentsView
	s.row = min(max(s.row+d, 0), max(len(s.rows)-1, 0))
	s.stopAsk = ""
	return m.scheduleAgentTerm()
}

func (m Model) handleAgentsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s, k := m.jiraTab.agentsView, m.keys
	if s.typing && m.agentsTermShown() {
		if key.Matches(msg, k.AgentBack) {
			s.typing = false
		} else {
			m.agentTerm.sendKey(msg)
		}
		return m, nil
	}
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
		m.closeAgentsTerm()
		m.jiraTab.agentsView = nil
		m.renderJira()
	case key.Matches(msg, k.Tab), key.Matches(msg, k.ShiftTab):
		s.bare = !s.bare
		m.buildAgentRows()
		return m, tea.Batch(m.lookUpAgentIssues(), m.scheduleAgentTerm())
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
		switch {
		case r.agent.PaneID != "" && m.agentsTermShown() && s.termFor == r.agent.PaneID:
			s.typing = true
			return m, nil
		case r.agent.PaneID != "" && !m.agentsSplit():
			return m, m.attachAgentIn(r.key, r.agent.PaneID, here)
		case r.agent.PaneID != "":
			return m, nil // still attaching
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

// clickAgents puts the cursor on the row clicked, a double click acting
// as enter; a click on the terminal types into it, the press reaching the
// agent when it takes the mouse.
func (m Model) clickAgents(msg tea.MouseClickMsg, count int) (tea.Model, tea.Cmd) {
	s := m.jiraTab.agentsView
	m.focus = focusJira
	if x, y, ok := m.agentTermCell(msg.X, msg.Y); ok && m.agentsTermShown() {
		s.typing = true
		if m.agentTerm.wantsMouse() {
			m.agentTermDrag = m.agentTerm.mouseEvent(termButton(msg.Mouse(), false), x, y, false)
		}
		return m, nil
	}
	i := s.rowAt(msg.X, msg.Y)
	if i < 0 {
		return m, nil
	}
	s.row, s.typing = i, false
	cmd := m.scheduleAgentTerm()
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
	keys := fmt.Sprintf("  ·  %s %s ◌ · %s type · %s issue · %s prompt · %s stop · %s browser · esc board",
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
	listW := m.agentsListW(width)
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

// renderAgentDetail draws the cursor's issue and agent over the agent's
// terminal.
func (m *Model) renderAgentDetail(width, height int) []string {
	s := m.jiraTab.agentsView
	r := s.cursor()
	if r.key == "" || width < 10 {
		return nil
	}
	is := s.issues[r.key]
	fit := func(l string) string { return ansi.Truncate(l, width, "…") }
	out := []string{fit(jiraKeyStyle.Render(r.key) + " " + titleStyle.Render(is.card.Summary))}
	var facts []string
	if is.found {
		facts = append(facts, is.card.Status, cmp.Or(is.card.Assignee, "unassigned"))
		if is.site != m.site {
			facts = append(facts, "on "+is.site)
		}
	}
	a := r.agent
	if a.PaneID == "" {
		facts = append(facts, "a worktree, no agent in it")
		return append(out, refDimStyle.Render(fit(strings.Join(facts, " · "))), "", refDimStyle.Render(fit("◌ "+homeShort(r.path))))
	}
	shown := m.agentsTermShown() && s.termFor == a.PaneID
	switch {
	case shown && s.typing:
		facts = append(facts, "typing · "+helpKey(m.keys.AgentBack)+" back to the list")
	case shown:
		facts = append(facts, helpKey(m.keys.OpenChannel)+" or a click to type")
	}
	out = append(out, fit(statusMark(a.Status)+" "+agentState(a.Status)+refDimStyle.Render("  "+strings.Join(facts, " · "))))
	switch {
	case !shown && s.termErr != "" && s.termFor == a.PaneID:
		return append(out, refDimStyle.Render(fit("  "+s.termErr)))
	case !shown:
		return append(out, refDimStyle.Render("  attaching…"))
	}
	w, h := m.agentsTermSize()
	m.agentTerm.resize(w, h)
	lines := strings.Split(m.agentTerm.view(), "\n")
	for i := range min(h, height-len(out)) {
		l := ""
		if i < len(lines) {
			l = ansi.Truncate(lines[i], width, "")
		}
		out = append(out, l)
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
