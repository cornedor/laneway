package ui

import (
	"fmt"
	"math"
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

// Sprint charts in place of the board: the active sprint's burndown (points
// left by resolution date, against the ideal line) and the velocity of the
// last closed sprints.

// Chart tabs, in their order.
const (
	chartBurndown = iota
	chartBurnup
	chartFlow
	chartVelocity
	chartTabs
)

type chartsState struct {
	tab     int // chartBurndown, chartBurnup or chartVelocity
	sprint  *jiraView
	burn    []jira.BurnIssue
	vel     []jira.SprintVelocity
	loading bool
	seq     int
	err     string
}

type chartsMsg struct {
	seq  int
	burn []jira.BurnIssue
	vel  []jira.SprintVelocity
	err  error
}

// openCharts swaps the board for the charts of its active sprint.
func (m *Model) openCharts() tea.Cmd {
	t := m.jiraTab
	if t.cfg == nil || t.board >= len(t.boards) || t.boards[t.board].Type != "scrum" {
		m.status = "charts need a scrum board"
		return nil
	}
	ch := &chartsState{}
	for _, v := range t.views {
		if v.kind == jiraViewSprint && v.lanes {
			ch.sprint = &v
			break
		}
	}
	if ch.sprint == nil {
		ch.tab = chartVelocity
	}
	t.charts = ch
	return m.loadCharts()
}

func (m *Model) loadCharts() tea.Cmd {
	t, ch := m.jiraTab, m.jiraTab.charts
	ch.seq++
	ch.loading = true
	seq, ctx, c, board, pf, n := ch.seq, m.ctx, m.jiraClient, m.jiraBoardID(), t.cfg.PointsField, m.opts.velocitySprints
	sprint := 0
	if ch.sprint != nil {
		sprint = ch.sprint.sprint
	}
	return func() tea.Msg {
		msg := chartsMsg{seq: seq}
		var errB, errV error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if sprint != 0 {
				msg.burn, errB = c.SprintBurn(ctx, sprint, pf)
			}
		}()
		go func() { defer wg.Done(); msg.vel, errV = c.Velocity(ctx, board, n, pf) }()
		wg.Wait()
		msg.err = firstErr(errB, errV)
		return msg
	}
}

func (m Model) handleCharts(msg chartsMsg) (tea.Model, tea.Cmd) {
	ch := m.jiraTab.charts
	if ch == nil || msg.seq != ch.seq {
		return m, nil
	}
	ch.loading = false
	if msg.err != nil {
		ch.err = msg.err.Error()
		return m, nil
	}
	ch.err, ch.burn, ch.vel = "", msg.burn, msg.vel
	return m, nil
}

func (m Model) handleChartsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ch := m.jiraTab.charts
	switch {
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, m.keys.Charts):
		m.jiraTab.charts = nil
		m.renderJira()
	case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.ShiftTab),
		key.Matches(msg, m.keys.PrevView), key.Matches(msg, m.keys.NextView):
		if ch.sprint != nil {
			d := 1
			if key.Matches(msg, m.keys.ShiftTab) || key.Matches(msg, m.keys.PrevView) {
				d = chartTabs - 1
			}
			ch.tab = (ch.tab + d) % chartTabs
		}
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadCharts()
	case key.Matches(msg, m.keys.CopyKey):
		if ch.loading || ch.err != "" {
			break
		}
		m.status = "copied the numbers as a markdown table"
		return m, tea.SetClipboard(m.chartTable(time.Now()))
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	}
	return m, nil
}

// chartTabNames are the charts' tab labels, by tab.
var chartTabNames = []string{"Burndown", "Burnup", "Flow", "Velocity"}

const chartTabSep = "  │  "

// chartTabsShown are the tabs the view line lists: all, or velocity alone
// without an active sprint.
func (ch *chartsState) chartTabsShown() []int {
	var out []int
	for i := range chartTabNames {
		if i == chartVelocity || ch.sprint != nil || i == ch.tab {
			out = append(out, i)
		}
	}
	return out
}

// chartsLine is the view line while the charts show.
func (m *Model) chartsLine() string { return joinSegs(m.chartsSegs()) }

// renderCharts draws the open chart into width × height.
func (m *Model) renderCharts(width, height int) string {
	ch := m.jiraTab.charts
	switch {
	case ch.err != "":
		return refErrStyle.Render(ch.err)
	case ch.loading && ch.burn == nil && ch.vel == nil:
		return refDimStyle.Render("loading…")
	case ch.tab == chartBurndown:
		return renderBurndown(*ch.sprint, ch.burn, time.Now(), width, height)
	case ch.tab == chartBurnup:
		return renderBurnup(*ch.sprint, ch.burn, time.Now(), width, height)
	case ch.tab == chartFlow:
		return renderFlow(*ch.sprint, ch.burn, m.jiraTab.cfg.Columns, time.Now(), width, height)
	}
	return renderVelocity(ch.vel, width)
}

// chartTable is the open chart's numbers as a markdown table: a row per day
// (per sprint for velocity).
func (m *Model) chartTable(now time.Time) string {
	ch := m.jiraTab.charts
	var head []string
	var rows [][]string
	day := func(i int) string { return ch.sprint.start.AddDate(0, 0, i).Format("2006-01-02") }
	switch ch.tab {
	case chartBurndown:
		head = []string{"Day", "Points left"}
		_, _, left := burnSeries(ch.burn, ch.sprint.start, ch.sprint.end, now)
		for i, l := range left {
			rows = append(rows, []string{day(i), chartNum(l)})
		}
	case chartBurnup:
		head = []string{"Day", "Scope", "Done"}
		scope, done := burnupSeries(ch.burn, ch.sprint.start, ch.sprint.end, now)
		for i := range scope {
			rows = append(rows, []string{day(i), chartNum(scope[i]), chartNum(done[i])})
		}
	case chartFlow:
		head = []string{"Day"}
		for _, c := range m.jiraTab.cfg.Columns {
			head = append(head, c.Name)
		}
		for i, counts := range flowSeries(ch.burn, m.jiraTab.cfg.Columns, ch.sprint.start, ch.sprint.end, now) {
			row := []string{day(i)}
			for _, n := range counts {
				row = append(row, strconv.Itoa(n))
			}
			rows = append(rows, row)
		}
	default:
		head = []string{"Sprint", "Committed", "Done"}
		for _, v := range ch.vel {
			rows = append(rows, []string{v.Name, chartNum(v.Committed), chartNum(v.Done)})
		}
	}
	return markdownTable(head, rows)
}

// markdownTable renders head and rows as a markdown table, escaping pipes.
func markdownTable(head []string, rows [][]string) string {
	cell := strings.NewReplacer("|", `\|`, "\n", " ").Replace
	var b strings.Builder
	line := func(cells []string) {
		for _, c := range cells {
			b.WriteString("| " + cell(c) + " ")
		}
		b.WriteString("|\n")
	}
	line(head)
	b.WriteString(strings.Repeat("|---", len(head)) + "|\n")
	for _, r := range rows {
		line(r)
	}
	return b.String()
}

// burnSeries is the sprint's points now, the points added after it started,
// and, per day from its start up to today (or its end), the points in it
// then and still open at that day's end.
func burnSeries(issues []jira.BurnIssue, start, end, now time.Time) (total, added float64, left []float64) {
	for _, is := range issues {
		total += is.Points
		if is.Added.After(start) {
			added += is.Points
		}
	}
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	last := end
	if now.Before(last) {
		last = now
	}
	for d := day; !d.After(last); d = d.AddDate(0, 0, 1) {
		eod := d.AddDate(0, 0, 1)
		open := 0.0
		for _, is := range issues {
			inScope := is.Added.IsZero() || is.Added.Before(eod)
			done := !is.Resolved.IsZero() && is.Resolved.Before(eod)
			if inScope && !done {
				open += is.Points
			}
		}
		left = append(left, open)
	}
	return total, added, left
}

// renderBurndown plots points left per day against the ideal line.
func renderBurndown(v jiraView, issues []jira.BurnIssue, now time.Time, width, height int) string {
	if v.start.IsZero() || v.end.IsZero() {
		return refDimStyle.Render(v.name + " has no dates")
	}
	total, added, left := burnSeries(issues, v.start, v.end, now)
	cur := total
	if len(left) > 0 {
		cur = left[len(left)-1]
	}
	scope := ""
	if added > 0 {
		scope = fmt.Sprintf(" · +%sp added since the start", chartNum(added))
	}
	title := jiraViewActive.Render(v.name) + jiraDimStyle.Render(fmt.Sprintf("  %s of %sp left%s · by resolution date", chartNum(cur), chartNum(total), scope))
	if total == 0 {
		return title + "\n\n" + refDimStyle.Render("no points in this sprint")
	}
	axisW := len(chartNum(total)) + 1
	cw, chh := max(width-axisW-1, 4), min(max(height-4, 3), 16)
	c := newBraille(cw, chh)
	dw, dh := c.dots()
	days := max(v.end.Sub(v.start).Hours()/24, 1)
	x := func(day float64) int { return int(math.Round(day / days * float64(dw-1))) }
	y := func(pts float64) int { return int(math.Round((1 - pts/total) * float64(dh-1))) }
	c.line(x(0), y(total), x(days), y(0), 3) // the ideal, dotted
	for i := 1; i < len(left); i++ {
		c.line(x(float64(i-1)), y(left[i-1]), x(float64(i)), y(left[i]), 1)
	}
	if len(left) == 1 {
		c.set(x(0), y(left[0]))
	}
	lines := []string{title, ""}
	for r, row := range c.rows() {
		label := ""
		switch r {
		case 0:
			label = chartNum(total)
		case chh - 1:
			label = "0"
		}
		lines = append(lines, jiraDimStyle.Render(fmt.Sprintf("%*s", axisW, label))+" "+roadmapTodoStyle.Render(row))
	}
	from, to := v.start.Format("Mon 2 Jan"), v.end.Format("Mon 2 Jan")
	lines = append(lines, strings.Repeat(" ", axisW+1)+jiraDimStyle.Render(from+strings.Repeat(" ", max(cw-len(from)-len(to), 1))+to))
	return strings.Join(lines, "\n")
}

// renderVelocity is a bar per closed sprint: done filled, the rest shaded.
func renderVelocity(vel []jira.SprintVelocity, width int) string {
	if len(vel) == 0 {
		return refDimStyle.Render("no closed sprints yet")
	}
	nameW, most, done := 0, 0.0, 0.0
	for _, s := range vel {
		nameW = max(nameW, len(s.Name))
		most = max(most, s.Committed, s.Done)
		done += s.Done
	}
	nameW = min(nameW, max(width/4, 8))
	lines := []string{jiraViewActive.Render("Velocity") + jiraDimStyle.Render(fmt.Sprintf("  last %d sprints · average %sp done", len(vel), chartNum(done/float64(len(vel))))), ""}
	barW := max(width-nameW-12, 4)
	for _, s := range vel {
		full, filled := 0, 0
		if most > 0 {
			full = int(math.Round(s.Committed / most * float64(barW)))
			filled = min(int(math.Round(s.Done/most*float64(barW))), full)
		}
		name := ansi.Truncate(s.Name, nameW, "…")
		bar := roadmapDoneStyle.Render(strings.Repeat("█", filled)) + roadmapTodoStyle.Render(strings.Repeat("▒", full-filled))
		lines = append(lines, fmt.Sprintf("%-*s ", nameW, name)+bar+jiraDimStyle.Render(fmt.Sprintf(" %s/%s", chartNum(s.Done), chartNum(s.Committed))))
	}
	return strings.Join(lines, "\n")
}

// chartNum is a point count without trailing zeros.
func chartNum(f float64) string {
	return strconv.FormatFloat(math.Round(f*10)/10, 'f', -1, 64)
}

// burnupSeries is, per day from the sprint's start up to today (or its end),
// the points in it then and the points of those done by that day's end.
func burnupSeries(issues []jira.BurnIssue, start, end, now time.Time) (scope, done []float64) {
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	last := end
	if now.Before(last) {
		last = now
	}
	for d := day; !d.After(last); d = d.AddDate(0, 0, 1) {
		eod := d.AddDate(0, 0, 1)
		s, dn := 0.0, 0.0
		for _, is := range issues {
			if !is.Added.IsZero() && !is.Added.Before(eod) {
				continue // not in the sprint yet
			}
			s += is.Points
			if !is.Resolved.IsZero() && is.Resolved.Before(eod) {
				dn += is.Points
			}
		}
		scope, done = append(scope, s), append(done, dn)
	}
	return scope, done
}

// renderBurnup plots points done against the sprint's scope (dotted).
func renderBurnup(v jiraView, issues []jira.BurnIssue, now time.Time, width, height int) string {
	if v.start.IsZero() || v.end.IsZero() {
		return refDimStyle.Render(v.name + " has no dates")
	}
	scope, done := burnupSeries(issues, v.start, v.end, now)
	top := 0.0
	for _, s := range scope {
		top = max(top, s)
	}
	cur, all := 0.0, 0.0
	if n := len(done); n > 0 {
		cur, all = done[n-1], scope[n-1]
	}
	title := jiraViewActive.Render(v.name) + jiraDimStyle.Render(fmt.Sprintf("  %s of %sp done · scope dotted", chartNum(cur), chartNum(all)))
	if top == 0 {
		return title + "\n\n" + refDimStyle.Render("no points in this sprint")
	}
	axisW := len(chartNum(top)) + 1
	cw, chh := max(width-axisW-1, 4), min(max(height-4, 3), 16)
	c := newBraille(cw, chh)
	dw, dh := c.dots()
	days := max(v.end.Sub(v.start).Hours()/24, 1)
	x := func(day float64) int { return int(math.Round(day / days * float64(dw-1))) }
	y := func(pts float64) int { return int(math.Round((1 - pts/top) * float64(dh-1))) }
	for i := 1; i < len(scope); i++ {
		c.line(x(float64(i-1)), y(scope[i-1]), x(float64(i)), y(scope[i]), 3)
		c.line(x(float64(i-1)), y(done[i-1]), x(float64(i)), y(done[i]), 1)
	}
	lines := []string{title, ""}
	for r, row := range c.rows() {
		label := ""
		switch r {
		case 0:
			label = chartNum(top)
		case chh - 1:
			label = "0"
		}
		lines = append(lines, jiraDimStyle.Render(fmt.Sprintf("%*s", axisW, label))+" "+roadmapDoneStyle.Render(row))
	}
	from, to := v.start.Format("Mon 2 Jan"), v.end.Format("Mon 2 Jan")
	lines = append(lines, strings.Repeat(" ", axisW+1)+jiraDimStyle.Render(from+strings.Repeat(" ", max(cw-len(from)-len(to), 1))+to))
	return strings.Join(lines, "\n")
}

// flowSeries is, per day of the sprint up to today, how many of its issues
// then stood in each board column (by the status each had at day's end).
func flowSeries(issues []jira.BurnIssue, cols []jira.Column, start, end, now time.Time) [][]int {
	colOf := map[string]int{}
	for i, c := range cols {
		for _, id := range c.StatusIDs {
			colOf[id] = i
		}
	}
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	last := end
	if now.Before(last) {
		last = now
	}
	var out [][]int
	for d := day; !d.After(last); d = d.AddDate(0, 0, 1) {
		eod := d.AddDate(0, 0, 1)
		counts := make([]int, len(cols))
		for _, is := range issues {
			if !is.Added.IsZero() && !is.Added.Before(eod) {
				continue
			}
			if c, ok := colOf[is.StatusAt(eod.Add(-time.Second))]; ok {
				counts[c]++
			}
		}
		out = append(out, counts)
	}
	return out
}

// flowStyle is column i's band colour: to do, then alternating middles,
// done last.
func flowStyle(i, n int) lipgloss.Style {
	switch {
	case i == n-1:
		return roadmapDoneStyle
	case i == 0:
		return roadmapTodoStyle
	case i%2 == 1:
		return roadmapTodayStyle
	}
	return jiraViewActive
}

// renderFlow draws the cumulative flow: a stacked band per board column,
// done at the bottom, one slice per day.
func renderFlow(v jiraView, issues []jira.BurnIssue, cols []jira.Column, now time.Time, width, height int) string {
	if v.start.IsZero() || v.end.IsZero() {
		return refDimStyle.Render(v.name + " has no dates")
	}
	days := flowSeries(issues, cols, v.start, v.end, now)
	top := 0
	for _, d := range days {
		sum := 0
		for _, n := range d {
			sum += n
		}
		top = max(top, sum)
	}
	var legend []string
	for i, c := range cols {
		legend = append(legend, flowStyle(i, len(cols)).Render("█ "+c.Name))
	}
	title := jiraViewActive.Render(v.name) + jiraDimStyle.Render("  issues per column, day by day   ") + strings.Join(legend, "  ")
	if top == 0 || len(days) == 0 {
		return title + "\n\n" + refDimStyle.Render("no issues in this sprint")
	}
	axisW := len(strconv.Itoa(top)) + 1
	cw, chh := max(width-axisW-1, 4), min(max(height-4, 3), 16)
	total := max(int(math.Ceil(v.end.Sub(v.start).Hours()/24)), len(days))
	lines := []string{title, ""}
	for r := 0; r < chh; r++ {
		level := float64(chh-r) / float64(chh) * float64(top) // this row's height
		var b strings.Builder
		for x := 0; x < cw; x++ {
			d := x * total / cw
			if d >= len(days) {
				b.WriteByte(' ')
				continue
			}
			cum, band := 0, -1
			for k := len(cols) - 1; k >= 0; k-- {
				cum += days[d][k]
				if float64(cum) >= level-float64(top)/float64(chh)/2 {
					band = k
					break
				}
			}
			if band < 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(flowStyle(band, len(cols)).Render("█"))
		}
		label := ""
		switch r {
		case 0:
			label = strconv.Itoa(top)
		case chh - 1:
			label = "0"
		}
		lines = append(lines, jiraDimStyle.Render(fmt.Sprintf("%*s", axisW, label))+" "+b.String())
	}
	from, to := v.start.Format("Mon 2 Jan"), v.end.Format("Mon 2 Jan")
	lines = append(lines, strings.Repeat(" ", axisW+1)+jiraDimStyle.Render(from+strings.Repeat(" ", max(cw-len(from)-len(to), 1))+to))
	return strings.Join(lines, "\n")
}
