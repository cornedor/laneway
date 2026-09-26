package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// Sprint planning: the backlog beside a sprint, in place of the board. Move
// cards across, rank them within a side, and see the sprint's points per
// assignee while you do. Moves and ranks show at once and are written in the
// background; a failed write reloads both sides.

type planState struct {
	sprints []jiraView // the board's sprint views, the targets
	target  int        // index into sprints
	sides   [2][]jira.Card
	side    int // 0 backlog, 1 sprint
	idx     [2]int
	top     [2]int
	loading bool
	seq     int
	err     string
	// closing is set by a first C: a second completes the active sprint.
	closing bool
	drag    planDrag
}

// planDrag is a card held by the mouse: armed on the press, active once
// the pointer leaves the cell, over the side under it.
type planDrag struct {
	key          string
	x, y         int
	side, over   int
	active, held bool
}

type planMsg struct {
	seq         int
	left, right []jira.Card
	err         error
}

// planWroteMsg is a move or rank answered.
type planWroteMsg struct {
	what string
	err  error
}

// openPlanning swaps the board for the planning view, aimed at the first
// future sprint (else the active one).
func (m *Model) openPlanning() tea.Cmd {
	t := m.jiraTab
	p := &planState{}
	for _, v := range t.views {
		if v.kind == jiraViewSprint {
			p.sprints = append(p.sprints, v)
		}
	}
	if len(p.sprints) == 0 || t.cfg == nil {
		m.status = "planning needs a scrum board with sprints"
		return nil
	}
	for i, v := range p.sprints {
		if !v.lanes { // not active: a future sprint
			p.target = i
			break
		}
	}
	t.plan = p
	return m.loadPlan()
}

func (m *Model) loadPlan() tea.Cmd {
	t, p := m.jiraTab, m.jiraTab.plan
	p.seq++
	p.loading = true
	seq, ctx, c, board, cfg, sprint := p.seq, m.ctx, m.jiraClient, m.jiraBoardID(), t.cfg, p.sprints[p.target]
	return func() tea.Msg {
		var msg planMsg
		var errL, errR error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			msg.left, _, errL = fetchJiraView(ctx, c, board, cfg, jiraView{kind: jiraViewBacklog}, "")
		}()
		go func() { defer wg.Done(); msg.right, _, errR = fetchJiraView(ctx, c, board, cfg, sprint, "") }()
		wg.Wait()
		msg.seq, msg.err = seq, firstErr(errL, errR)
		return msg
	}
}

func (m Model) handlePlan(msg planMsg) (tea.Model, tea.Cmd) {
	p := m.jiraTab.plan
	if p == nil || msg.seq != p.seq {
		return m, nil
	}
	p.loading = false
	if msg.err != nil {
		p.err = msg.err.Error()
		return m, nil
	}
	p.err = ""
	p.sides = [2][]jira.Card{msg.left, msg.right}
	for s := range p.sides {
		p.idx[s] = min(p.idx[s], max(len(p.sides[s])-1, 0))
	}
	return m, nil
}

func (m Model) handlePlanWrote(msg planWroteMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil {
		m.status = msg.what
		return m, nil
	}
	m.status = "not saved: " + msg.err.Error()
	if m.jiraTab.plan != nil {
		return m, m.loadPlan()
	}
	return m, nil
}

// planCard is the selected card of the active side.
func (p *planState) planCard() (jira.Card, bool) {
	s := p.sides[p.side]
	if i := p.idx[p.side]; i < len(s) {
		return s[i], true
	}
	return jira.Card{}, false
}

func (m Model) handlePlanKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t, p := m.jiraTab, m.jiraTab.plan
	n := len(p.sides[p.side])
	if msg.String() != "C" {
		p.closing = false // a completion is confirmed by the very next key only
	}
	switch {
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, m.keys.Plan):
		t.plan = nil
		m.renderJira()
		return m, m.refreshJiraAfterEdit()
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.InputUp):
		p.idx[p.side] = max(p.idx[p.side]-1, 0)
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.InputDown):
		p.idx[p.side] = min(p.idx[p.side]+1, max(n-1, 0))
	case key.Matches(msg, m.keys.Home):
		p.idx[p.side] = 0
	case key.Matches(msg, m.keys.End):
		p.idx[p.side] = max(n-1, 0)
	case key.Matches(msg, m.keys.PageUp):
		p.idx[p.side] = max(p.idx[p.side]-10, 0)
	case key.Matches(msg, m.keys.PageDown):
		p.idx[p.side] = min(p.idx[p.side]+10, max(n-1, 0))
	case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Right),
		key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.ShiftTab):
		p.side = 1 - p.side
	case key.Matches(msg, m.keys.PrevView), key.Matches(msg, m.keys.NextView):
		d := 1
		if key.Matches(msg, m.keys.PrevView) {
			d = -1
		}
		if len(p.sprints) > 1 {
			p.target = (p.target + d + len(p.sprints)) % len(p.sprints)
			p.sides[1], p.idx[1], p.top[1] = nil, 0, 0
			return m, m.loadPlan()
		}
	case key.Matches(msg, m.keys.Mark):
		if c, ok := p.planCard(); ok {
			if t.marked == nil {
				t.marked = map[string]bool{}
			}
			if t.marked[c.Key] {
				delete(t.marked, c.Key)
			} else {
				t.marked[c.Key] = true
			}
			p.idx[p.side] = min(p.idx[p.side]+1, max(n-1, 0))
		}
	case key.Matches(msg, m.keys.MoveSprint), msg.String() == "space":
		return m, m.planMove()
	case msg.String() == "S":
		return m, m.planStart()
	case msg.String() == "E":
		v := p.sprints[p.target]
		m.openBulkInput("plan-goal", "the sprint's goal (empty clears)")
		m.jiraFieldInput.SetValue(v.goal)
		m.jiraFieldInput.CursorEnd()
		m.jiraFieldKey = v.name
		return m, nil
	case msg.String() == "R":
		m.openBulkInput("plan-rename", "sprint name")
		m.jiraFieldInput.SetValue(p.sprints[p.target].name)
		m.jiraFieldInput.CursorEnd()
		return m, nil
	case msg.String() == "N":
		m.openBulkInput("plan-new", "sprint name")
		m.jiraFieldInput.SetValue(nextSprintName(p.sprints))
		m.jiraFieldInput.CursorEnd()
		return m, nil
	case msg.String() == "C":
		return m, m.planClose()
	case msg.String() == "K":
		return m, m.planRank(-1)
	case msg.String() == "J":
		return m, m.planRank(1)
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadPlan()
	case key.Matches(msg, m.keys.OpenAttach):
		if c, ok := p.planCard(); ok {
			url := m.jiraClient.BrowseURL(c.Key)
			m.status = "opening " + url + "…"
			return m, m.openOpenable(openable{name: c.Key, url: url})
		}
	case key.Matches(msg, m.keys.OpenChannel), key.Matches(msg, m.keys.OpenRef):
		if c, ok := p.planCard(); ok {
			return m.openJiraKey(c.Key)
		}
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	}
	return m, nil
}

// planMove takes the marked cards of the side (else the selected one)
// across: into the sprint at its end, or back to the top of the backlog, as
// Jira places them.
func (m *Model) planMove() tea.Cmd {
	return m.planMoveOf(func(c jira.Card) bool { return m.jiraTab.marked[c.Key] })
}

// planMoveOf moves the side's cards take picks, else the selected one.
func (m *Model) planMoveOf(take func(jira.Card) bool) tea.Cmd {
	t, p := m.jiraTab, m.jiraTab.plan
	from, to := p.side, 1-p.side
	var moving, staying []jira.Card
	for _, c := range p.sides[from] {
		if take(c) {
			moving = append(moving, c)
		} else {
			staying = append(staying, c)
		}
	}
	if len(moving) == 0 {
		c, ok := p.planCard()
		if !ok {
			return nil
		}
		moving, staying = []jira.Card{c}, slices.DeleteFunc(slices.Clone(p.sides[from]), func(x jira.Card) bool { return x.Key == c.Key })
	}
	keys := make([]string, len(moving))
	for i, c := range moving {
		keys[i] = c.Key
		delete(t.marked, c.Key)
	}
	p.sides[from] = staying
	p.idx[from] = min(p.idx[from], max(len(staying)-1, 0))
	what := strings.Join(keys, ", ")
	client, ctx, sprint := m.jiraClient, m.ctx, p.sprints[p.target]
	if to == 1 {
		p.sides[1] = append(p.sides[1], moving...)
		m.status = "moving " + what + " to " + sprint.name + "…"
		return planWrite(what+" → "+sprint.name, func() error { return client.MoveToSprint(ctx, sprint.sprint, keys...) })
	}
	p.sides[0] = append(slices.Clone(moving), p.sides[0]...)
	m.status = "moving " + what + " to the backlog…"
	return planWrite(what+" → backlog", func() error { return client.MoveToBacklog(ctx, keys...) })
}

// planStart asks when the target sprint ends, to start it now.
func (m *Model) planStart() tea.Cmd {
	p := m.jiraTab.plan
	v := p.sprints[p.target]
	if v.lanes { // active: move its end instead
		m.openBulkInput("plan-end", "new end: 2026-10-10, +3d, fri")
		m.jiraFieldKey = v.name
		return nil
	}
	m.openBulkInput("plan-start", "end: 2026-10-10, +2w, fri")
	m.jiraFieldInput.SetValue("+2w")
	m.jiraFieldInput.CursorEnd()
	m.jiraFieldKey = v.name
	return nil
}

// applyPlanStart starts the target sprint today, ending on the typed day.
func (m Model) applyPlanStart(raw string) (tea.Model, tea.Cmd) {
	p := m.jiraTab.plan
	if p == nil {
		m.closeJiraField()
		return m, nil
	}
	now := time.Now()
	end, err := jira.ParseDate(raw, now)
	if err != nil || !end.After(now) {
		m.status = "not a day after today: " + raw
		return m, nil
	}
	m.closeJiraField()
	v, c, ctx := p.sprints[p.target], m.jiraClient, m.ctx
	end = time.Date(end.Year(), end.Month(), end.Day(), 17, 0, 0, 0, end.Location())
	m.status = "starting " + v.name + "…"
	return m, func() tea.Msg {
		err := c.StartSprint(ctx, v.sprint, now, end)
		return planSprintMsg{what: v.name + " started, ends " + end.Format("Mon 2 Jan"), err: err}
	}
}

// nextSprintName numbers on from the last sprint: "ABC Sprint 12" gives
// "ABC Sprint 13"; without a number, "" to type one.
func nextSprintName(sprints []jiraView) string {
	if len(sprints) == 0 {
		return ""
	}
	last := sprints[len(sprints)-1].name
	i := len(last)
	for i > 0 && last[i-1] >= '0' && last[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(last[i:])
	if err != nil {
		return ""
	}
	return last[:i] + strconv.Itoa(n+1)
}

// applyPlanSprint renames the target sprint or moves its end, as field says.
func (m Model) applyPlanSprint(field, raw string) (tea.Model, tea.Cmd) {
	p := m.jiraTab.plan
	if p == nil {
		m.closeJiraField()
		return m, nil
	}
	v, c, ctx := p.sprints[p.target], m.jiraClient, m.ctx
	name, end, what := "", time.Time{}, ""
	if field == "plan-rename" {
		if name = strings.TrimSpace(raw); name == "" {
			m.status = "a sprint needs a name"
			return m, nil
		}
		what = v.name + " renamed to " + name
	} else {
		d, err := jira.ParseDate(raw, time.Now())
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		end = time.Date(d.Year(), d.Month(), d.Day(), 17, 0, 0, 0, d.Location())
		what = v.name + " now ends " + end.Format("Mon 2 Jan")
	}
	m.closeJiraField()
	return m, func() tea.Msg {
		return planSprintMsg{what: what, err: c.UpdateSprint(ctx, v.sprint, name, end)}
	}
}

// applyPlanGoal sets the target sprint's goal.
func (m Model) applyPlanGoal(raw string) (tea.Model, tea.Cmd) {
	p := m.jiraTab.plan
	m.closeJiraField()
	if p == nil {
		return m, nil
	}
	v, goal, c, ctx := p.sprints[p.target], strings.TrimSpace(raw), m.jiraClient, m.ctx
	m.status = "setting the goal of " + v.name + "…"
	return m, func() tea.Msg {
		return planSprintMsg{what: v.name + " goal set", err: c.SetSprintGoal(ctx, v.sprint, goal)}
	}
}

// applyPlanNew creates a sprint named raw on the board.
func (m Model) applyPlanNew(raw string) (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(raw)
	if name == "" {
		m.status = "a sprint needs a name"
		return m, nil
	}
	m.closeJiraField()
	c, ctx, board := m.jiraClient, m.ctx, m.jiraBoardID()
	m.status = "creating " + name + "…"
	return m, func() tea.Msg {
		return planSprintMsg{what: name + " created", err: c.CreateSprint(ctx, board, name)}
	}
}

// planClose completes the active sprint on a second C: its unfinished
// issues (not in the board's last column) go to the next planned sprint,
// else the backlog, then it closes.
func (m *Model) planClose() tea.Cmd {
	t, p := m.jiraTab, m.jiraTab.plan
	ai := slices.IndexFunc(p.sprints, func(v jiraView) bool { return v.lanes })
	if ai < 0 {
		m.status = "no active sprint"
		return nil
	}
	active := p.sprints[ai]
	next := -1
	for i := ai + 1; i < len(p.sprints); i++ {
		if !p.sprints[i].lanes {
			next = i
			break
		}
	}
	dest := "the backlog"
	if next >= 0 {
		dest = p.sprints[next].name
	}
	if !p.closing {
		p.closing = true
		m.status = "C again completes " + active.name + ", unfinished issues to " + dest
		return nil
	}
	p.closing = false
	var done []string
	if cols := t.cfg.Columns; len(cols) > 0 {
		done = cols[len(cols)-1].StatusIDs
	}
	c, ctx, board, cfg := m.jiraClient, m.ctx, m.jiraBoardID(), t.cfg
	nextID := 0
	if next >= 0 {
		nextID = p.sprints[next].sprint
	}
	m.status = "completing " + active.name + "…"
	return func() tea.Msg {
		// Only the unfinished are fetched, round after round until none are
		// left, so a sprint bigger than the card limit still empties.
		filter := ""
		if len(done) > 0 {
			filter = "status not in (" + strings.Join(done, ",") + ")"
		}
		moved := 0
		for round := 0; round < 20; round++ {
			cards, _, err := fetchJiraView(ctx, c, board, cfg, active, filter)
			if err != nil {
				return planSprintMsg{err: err}
			}
			var open []string
			for _, cd := range cards {
				if !slices.Contains(done, cd.StatusID) {
					open = append(open, cd.Key)
				}
			}
			if len(open) == 0 {
				break
			}
			if nextID != 0 {
				err = c.MoveToSprint(ctx, nextID, open...)
			} else {
				err = c.MoveToBacklog(ctx, open...)
			}
			if err != nil {
				return planSprintMsg{err: fmt.Errorf("moving unfinished issues: %w", err)}
			}
			moved += len(open)
		}
		if err := c.CloseSprint(ctx, active.sprint); err != nil {
			return planSprintMsg{err: err}
		}
		return planSprintMsg{what: fmt.Sprintf("%s completed, %d unfinished to %s", active.name, moved, dest)}
	}
}

// planSprintMsg is a sprint started or completed; the board reloads, its
// sprints having changed.
type planSprintMsg struct {
	what string
	err  error
}

func (m Model) handlePlanSprint(msg planSprintMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "sprint: " + msg.err.Error()
		return m, nil
	}
	m.status = msg.what
	t := m.jiraTab
	t.plan = nil
	return m, m.loadJiraBoard(t.project, m.jiraBoardID(), "", false)
}

// planRank swaps the selected card with its neighbour d away and ranks it
// before (up) or after (down) that neighbour.
func (m *Model) planRank(d int) tea.Cmd {
	p := m.jiraTab.plan
	s, i := p.sides[p.side], p.idx[p.side]
	j := i + d
	if i >= len(s) || j < 0 || j >= len(s) {
		return nil
	}
	s = slices.Clone(s)
	s[i], s[j] = s[j], s[i]
	p.sides[p.side], p.idx[p.side] = s, j
	key, other := s[j].Key, s[i].Key
	client, ctx := m.jiraClient, m.ctx
	return planWrite(key+" ranked", func() error { return client.Rank(ctx, key, other, d > 0) })
}

func planWrite(what string, run func() error) tea.Cmd {
	return func() tea.Msg { return planWroteMsg{what: what, err: run()} }
}

// planLine is the view line while planning shows.
func (m *Model) planLine() string { return joinSegs(m.planSegs()) }

// renderPlan draws the two sides into width × height.
func (m *Model) renderPlan(width, height int) string {
	p := m.jiraTab.plan
	switch {
	case p.err != "":
		return refErrStyle.Render(p.err)
	case p.loading && p.sides[0] == nil && p.sides[1] == nil:
		return refDimStyle.Render("loading…")
	}
	leftW := (width - 3) / 2
	rightW := width - 3 - leftW
	left := m.renderPlanSide(0, "Backlog", leftW, height)
	right := m.renderPlanSide(1, p.sprints[p.target].name, rightW, height)
	sep := strings.TrimSuffix(strings.Repeat(jiraDimStyle.Render(" │ ")+"\n", height), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
}

// renderPlanSide is one side: its head (cards, points; per assignee for the
// sprint) and its rows, windowed around the cursor.
func (m *Model) renderPlanSide(side int, name string, width, height int) string {
	p := m.jiraTab.plan
	cards := p.sides[side]
	pts, _ := planPoints(cards)
	outer := width
	width = max(width-1, 1) // a cell of air before the divider or border
	headStyle, drop := jiraDimStyle, ""
	if side == p.side {
		headStyle = jiraViewActive
	}
	if p.drag.active && p.drag.over == side && side != p.drag.side {
		headStyle, drop = jiraViewActive, "  ◂ drop"
	}
	lines := []string{headStyle.Render(ansi.Truncate(fmt.Sprintf("%s  %d cards · %sp%s", name, len(cards), pts, drop), width, "…"))}
	if side == 1 {
		lines = append(lines, ansi.Truncate(planByAssignee(cards, m.opts.capacity), width, "…"))
	} else {
		lines = append(lines, "")
	}
	shown := max(height-len(lines), 1)
	i := p.idx[side]
	p.top[side] = min(max(p.top[side], i-shown+1), i)
	keyW := 0
	for _, c := range cards {
		keyW = max(keyW, len(c.Key))
	}
	for r := p.top[side]; r < len(cards) && r < p.top[side]+shown; r++ {
		c := cards[r]
		ptsCol := fmt.Sprintf("%4s", c.Points)
		mark := " "
		if mk := m.jiraMark(c.Key); mk != "" {
			mark = mk
		}
		row := mark + fmt.Sprintf("%-*s ", keyW, c.Key) + jiraTypeIcon(c.Type) + " "
		row += ansi.Truncate(c.Summary, max(width-lipgloss.Width(row)-len(ptsCol)-1, 1), "…")
		row += strings.Repeat(" ", max(width-lipgloss.Width(row)-len(ptsCol), 0)) + ptsCol
		switch {
		case p.drag.active && c.Key == p.drag.key: // being dragged: faint where it was
			row = jiraGhostStyle.Render(ansi.Truncate("┊ "+c.Key+" "+c.Summary, width, "…"))
		case r == i && side == p.side:
			row = selectedRow.Render(ansi.Strip(row))
		case r == i:
			row = diffTreeSelStyle.Render(ansi.Strip(row))
		}
		lines = append(lines, row)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(outer).Render(strings.Join(lines, "\n"))
}

// planPoints sums the cards' points.
func planPoints(cards []jira.Card) (string, float64) {
	sum := 0.0
	for _, c := range cards {
		if f, err := strconv.ParseFloat(c.Points, 64); err == nil {
			sum += f
		}
	}
	sum = math.Round(sum*100) / 100
	return strconv.FormatFloat(sum, 'f', -1, 64), sum
}

// planByAssignee is the points per assignee, most first: "Ada 13 · — 5",
// against their capacity when one is set ("Ada 13/10", red when over).
func planByAssignee(cards []jira.Card, capacity map[string]float64) string {
	by := map[string][]jira.Card{}
	for _, c := range cards {
		name := c.Assignee
		if name == "" {
			name = "—"
		}
		by[name] = append(by[name], c)
	}
	type share struct {
		name string
		pts  float64
		s    string
	}
	var shares []share
	for name, cs := range by {
		s, f := planPoints(cs)
		shares = append(shares, share{name, f, s})
	}
	slices.SortFunc(shares, func(a, b share) int {
		if a.pts != b.pts {
			if a.pts > b.pts {
				return -1
			}
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	parts := make([]string, len(shares))
	for i, s := range shares {
		cp, ok := capacity[s.name]
		if !ok && s.name != "—" {
			cp, ok = capacity["default"]
		}
		switch {
		case !ok:
			parts[i] = jiraDimStyle.Render(s.name + " " + s.s)
		case s.pts > cp:
			parts[i] = jiraOverStyle.Render(fmt.Sprintf("%s %s/%s", s.name, s.s, chartNum(cp)))
		default:
			parts[i] = jiraDimStyle.Render(fmt.Sprintf("%s %s/%s", s.name, s.s, chartNum(cp)))
		}
	}
	return strings.Join(parts, jiraDimStyle.Render(" · "))
}
