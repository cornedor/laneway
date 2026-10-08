package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// W in today's worklogs opens the week in place of the board: an issue per
// row, a day per column, with the day's and the week's totals and how far
// a workday is short of 8h. enter on a cell logs work on that issue that
// day, # adds an issue's row to log on; [ ] step a week, y copies the grid
// as a markdown table.

// workdaySecs is the day a gap is measured against.
const workdaySecs = 8 * 3600

type weekState struct {
	from     time.Time // the Monday, midnight
	rows     []weekRow
	row, col int // the cursor: an issue row, a day 0-6
	loading  bool
	err      string
	seq      int
}

// weekRow is an issue's time per day of the week.
type weekRow struct {
	key, summary string
	secs         [7]int
}

type weekMsg struct {
	seq  int
	logs []jira.Worklog
	err  error
}

// weekStart is the Monday of t's week, at midnight.
func weekStart(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// openWeek swaps the board for day's week.
func (m *Model) openWeek(day time.Time) tea.Cmd {
	m.jiraTab.week = &weekState{from: weekStart(day), col: (int(day.Weekday()) + 6) % 7}
	return m.loadWeek()
}

func (m *Model) loadWeek() tea.Cmd {
	w := m.jiraTab.week
	w.seq++
	w.loading = true
	seq, from, c, ctx := w.seq, w.from, m.jiraClient, m.ctx
	return func() tea.Msg {
		logs, err := c.MyWorklogsBetween(ctx, from, from.AddDate(0, 0, 7))
		return weekMsg{seq: seq, logs: logs, err: err}
	}
}

func (m Model) handleWeek(msg weekMsg) (tea.Model, tea.Cmd) {
	w := m.jiraTab.week
	if w == nil || msg.seq != w.seq {
		return m, nil
	}
	w.loading = false
	if msg.err != nil {
		w.err = msg.err.Error()
		return m, nil
	}
	w.err = ""
	w.rows = weekRows(msg.logs, w.from)
	w.row = min(w.row, max(len(w.rows)-1, 0))
	return m, nil
}

// weekRows sums logs per issue and day, the issues by key.
func weekRows(logs []jira.Worklog, from time.Time) []weekRow {
	var rows []weekRow
	for _, l := range logs {
		d := int(l.Started.In(from.Location()).Sub(from).Hours() / 24)
		if d < 0 || d > 6 {
			continue
		}
		i := slices.IndexFunc(rows, func(r weekRow) bool { return r.key == l.Key })
		if i < 0 {
			rows = append(rows, weekRow{key: l.Key, summary: l.Summary})
			i = len(rows) - 1
		}
		rows[i].secs[d] += l.Seconds
	}
	slices.SortFunc(rows, func(a, b weekRow) int { return compareKeys(a.key, b.key) })
	return rows
}

// compareKeys orders ABC-2 before ABC-10.
func compareKeys(a, b string) int {
	pa, na, _ := strings.Cut(a, "-")
	pb, nb, _ := strings.Cut(b, "-")
	if c := strings.Compare(pa, pb); c != 0 {
		return c
	}
	if len(na) != len(nb) {
		return len(na) - len(nb)
	}
	return strings.Compare(na, nb)
}

// weekTotals are the day totals and the week's.
func weekTotals(rows []weekRow) (days [7]int, week int) {
	for _, r := range rows {
		for d, s := range r.secs {
			days[d] += s
			week += s
		}
	}
	return days, week
}

// workday reports whether the week's day d (0 Monday) is one of workdays.
func (m *Model) workday(w *weekState, d int) bool {
	wd := w.from.AddDate(0, 0, d).Weekday()
	if len(m.opts.workdays) == 0 {
		return wd != time.Saturday && wd != time.Sunday
	}
	return slices.Contains(m.opts.workdays, wd)
}

func (m Model) handleWeekKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	w := m.jiraTab.week
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Timesheet):
		m.jiraTab.week = nil
		m.renderJira()
	case key.Matches(msg, m.keys.Up):
		w.row = max(w.row-1, 0)
	case key.Matches(msg, m.keys.Down):
		w.row = min(w.row+1, max(len(w.rows)-1, 0))
	case key.Matches(msg, m.keys.Left):
		w.col = max(w.col-1, 0)
	case key.Matches(msg, m.keys.Right):
		w.col = min(w.col+1, 6)
	case key.Matches(msg, m.keys.PrevView):
		w.from = w.from.AddDate(0, 0, -7)
		return m, m.loadWeek()
	case key.Matches(msg, m.keys.NextView):
		if !w.from.AddDate(0, 0, 7).After(time.Now()) {
			w.from = w.from.AddDate(0, 0, 7)
			return m, m.loadWeek()
		}
		m.status = i18n.T("this week is the last to show")
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadWeek()
	case key.Matches(msg, m.keys.OpenChannel):
		if w.row >= len(w.rows) {
			m.status = i18n.Tf("nothing logged this week · %s in the panel logs work", helpKey(m.keys.LogWork))
			break
		}
		r := w.rows[w.row]
		m.openWorklogInput(r.key, "", w.from.AddDate(0, 0, w.col).Add(m.opts.workdayStart))
	case key.Matches(msg, m.keys.Goto):
		m.openBulkInput("week-add", i18n.T("ABC-123 or 123"))
		m.jiraFieldKey = i18n.T("the week")
	case key.Matches(msg, m.keys.CopyKey):
		if w.loading || w.err != "" {
			break
		}
		m.status = i18n.T("copied the week as a markdown table")
		return m, tea.SetClipboard(m.weekTable())
	case key.Matches(msg, m.keys.Help):
		m.openHelp(i18n.T("Week"))
	}
	return m, nil
}

// applyWeekAdd adds the typed issue's row to the week, the cursor on it;
// one not on the board is looked up for its summary first.
func (m Model) applyWeekAdd(raw string) (tea.Model, tea.Cmd) {
	k := jiraGotoKey(raw, m.jiraTab.project)
	if k == "" {
		m.fail(i18n.Tf("not an issue key: %s", raw))
		return m, nil
	}
	m.closeJiraField()
	if c, ok := m.cardOf(k); ok {
		m.addWeekRow(k, c.Summary)
		return m, nil
	}
	client, ctx := m.jiraClient, m.ctx
	m.status = i18n.Tf("looking up %s…", k)
	return m, func() tea.Msg {
		iss, err := client.Get(ctx, k)
		msg := weekAddMsg{key: k, err: err}
		if iss != nil {
			msg.summary = iss.Summary
		}
		return msg
	}
}

// weekAddMsg is an issue looked up to add to the week.
type weekAddMsg struct {
	key, summary string
	err          error
}

func (m Model) handleWeekAdd(msg weekAddMsg) (tea.Model, tea.Cmd) {
	if m.jiraTab.week == nil {
		return m, nil
	}
	if msg.err != nil {
		m.fail(msg.err.Error())
		return m, nil
	}
	m.status = ""
	m.addWeekRow(msg.key, msg.summary)
	return m, nil
}

// addWeekRow puts the cursor on key's row, adding an empty one when the
// week has none, in today's column when the week is this one.
func (m *Model) addWeekRow(key, summary string) {
	w := m.jiraTab.week
	i := slices.IndexFunc(w.rows, func(r weekRow) bool { return r.key == key })
	if i < 0 {
		w.rows = append(w.rows, weekRow{key: key, summary: summary})
		slices.SortFunc(w.rows, func(a, b weekRow) int { return compareKeys(a.key, b.key) })
		i = slices.IndexFunc(w.rows, func(r weekRow) bool { return r.key == key })
	}
	w.row = i
	if d := int(time.Since(w.from).Hours() / 24); d >= 0 && d < 7 {
		w.col = d
	}
}

// weekCell is secs as a cell: "1h 30m", "·" for none.
func weekCell(secs int) string {
	if secs == 0 {
		return "·"
	}
	return jira.FormatDuration(secs)
}

// weekGap is how far a workday is short of 8h: "-1h 30m", "" when it isn't.
func weekGap(secs int) string {
	if secs >= workdaySecs {
		return ""
	}
	return "-" + jira.FormatDuration(workdaySecs-secs)
}

// weekLine is the view line: the week and its total.
func (m *Model) weekLine() string {
	w := m.jiraTab.week
	_, total := weekTotals(w.rows)
	s := jiraViewActive.Render(i18n.Tf("Week of %s", w.from.Format("Mon 2 Jan"))) + jiraDimStyle.Render("  ·  "+jira.FormatDuration(total))
	if w.loading {
		s += jiraDimStyle.Render(i18n.T("  ·  loading…"))
	}
	k := m.keys
	return s + jiraDimStyle.Render(i18n.Tf("  ·  %s %s week · enter log in the cell · %s add an issue · %s copy · esc board",
		helpKey(k.PrevView), helpKey(k.NextView), helpKey(k.Goto), helpKey(k.CopyKey)))
}

const weekColW = 8

// renderWeek draws the grid into width × height.
func (m *Model) renderWeek(width, height int) string {
	w := m.jiraTab.week
	switch {
	case w.err != "":
		s, _ := jiraErrorState(w.err, width, height, m.screenErrHints()...)
		return s
	case w.loading && w.rows == nil:
		return refDimStyle.Render(i18n.T("loading…"))
	}
	nameW := max(width-8*weekColW-2, 12)
	cell := func(s string, st lipgloss.Style) string {
		return st.Render(fmt.Sprintf("%*s", weekColW, ansi.Truncate(s, weekColW-1, "…")))
	}
	head := fmt.Sprintf("%-*s", nameW, i18n.T("Issue"))
	for d := range 7 {
		head += cell(w.from.AddDate(0, 0, d).Format("Mon 2"), jiraDimStyle)
	}
	lines := []string{jiraDimStyle.Render(head) + cell(i18n.T("Total"), jiraDimStyle)}
	for i, r := range w.rows {
		name := ansi.Truncate(jiraKeyStyle.Render(r.key)+" "+r.summary, nameW-1, "…")
		line := name + strings.Repeat(" ", max(nameW-ansi.StringWidth(name), 0))
		sum := 0
		for d, s := range r.secs {
			sum += s
			st := lipgloss.NewStyle()
			if s == 0 {
				st = jiraDimStyle
			}
			if i == w.row && d == w.col {
				st = st.Reverse(true)
			}
			line += cell(weekCell(s), st)
		}
		lines = append(lines, line+cell(jira.FormatDuration(sum), lipgloss.NewStyle().Bold(true)))
	}
	if len(w.rows) == 0 {
		lines = append(lines, jiraDimStyle.Render(i18n.Tf("nothing logged this week · %s in the panel logs work", helpKey(m.keys.LogWork))))
	}
	days, total := weekTotals(w.rows)
	tot := fmt.Sprintf("%-*s", nameW, i18n.T("Total"))
	gap := fmt.Sprintf("%-*s", nameW, i18n.T("Short of 8h"))
	for d, s := range days {
		tot += cell(weekCell(s), lipgloss.NewStyle().Bold(true))
		g := ""
		// Days gone by only: today isn't short yet.
		if m.workday(w, d) && w.from.AddDate(0, 0, d+1).Before(time.Now()) {
			g = weekGap(s)
		}
		gap += cell(g, jiraOverStyle)
	}
	lines = append(lines, "", tot+cell(jira.FormatDuration(total), lipgloss.NewStyle().Bold(true)), gap)
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

// weekTable is the grid as a markdown table.
func (m *Model) weekTable() string {
	w := m.jiraTab.week
	head := []string{"Issue", "Summary"}
	for d := range 7 {
		head = append(head, w.from.AddDate(0, 0, d).Format("Mon 2"))
	}
	head = append(head, "Total")
	var rows [][]string
	for _, r := range w.rows {
		row := []string{r.key, r.summary}
		sum := 0
		for _, s := range r.secs {
			sum += s
			row = append(row, weekCell(s))
		}
		rows = append(rows, append(row, jira.FormatDuration(sum)))
	}
	days, total := weekTotals(w.rows)
	row := []string{"", "Total"}
	for _, s := range days {
		row = append(row, weekCell(s))
	}
	rows = append(rows, append(row, jira.FormatDuration(total)))
	return markdownTable(head, rows)
}
