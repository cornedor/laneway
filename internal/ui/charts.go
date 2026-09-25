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
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// Sprint charts in place of the board: the active sprint's burndown (points
// left by resolution date, against the ideal line) and the velocity of the
// last closed sprints.

const velocitySprints = 8

type chartsState struct {
	tab     int // 0 burndown, 1 velocity
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
		ch.tab = 1
	}
	t.charts = ch
	return m.loadCharts()
}

func (m *Model) loadCharts() tea.Cmd {
	t, ch := m.jiraTab, m.jiraTab.charts
	ch.seq++
	ch.loading = true
	seq, ctx, c, board, pf := ch.seq, m.ctx, m.jiraClient, m.jiraBoardID(), t.cfg.PointsField
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
		go func() { defer wg.Done(); msg.vel, errV = c.Velocity(ctx, board, velocitySprints, pf) }()
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
		return m, tea.Quit
	case msg.String() == "esc", key.Matches(msg, m.keys.Charts):
		m.jiraTab.charts = nil
		m.renderJira()
	case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.ShiftTab),
		key.Matches(msg, m.keys.PrevView), key.Matches(msg, m.keys.NextView):
		if ch.sprint != nil {
			ch.tab = 1 - ch.tab
		}
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadCharts()
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	}
	return m, nil
}

// chartsLine is the view line while the charts show.
func (m *Model) chartsLine() string {
	ch := m.jiraTab.charts
	tabs := []string{"Burndown", "Velocity"}
	var parts []string
	for i, name := range tabs {
		switch {
		case i == ch.tab:
			parts = append(parts, jiraViewActive.Render(name))
		case i == 0 && ch.sprint == nil:
			continue
		default:
			parts = append(parts, jiraDimStyle.Render(name))
		}
	}
	s := strings.Join(parts, jiraDimStyle.Render("  │  "))
	if ch.loading {
		s += jiraDimStyle.Render("  ·  loading…")
	}
	return s + jiraDimStyle.Render("  ·  tab switch  "+helpKey(m.keys.Refresh)+" refresh  esc board")
}

// renderCharts draws the open chart into width × height.
func (m *Model) renderCharts(width, height int) string {
	ch := m.jiraTab.charts
	switch {
	case ch.err != "":
		return refErrStyle.Render(ch.err)
	case ch.loading && ch.burn == nil && ch.vel == nil:
		return refDimStyle.Render("loading…")
	case ch.tab == 0:
		return renderBurndown(*ch.sprint, ch.burn, time.Now(), width, height)
	}
	return renderVelocity(ch.vel, width)
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
