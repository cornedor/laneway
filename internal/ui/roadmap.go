package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// The roadmap: the project's epics as bars on a timeline, in place of the
// board's cards. A bar runs from the epic's start to its due date (or its
// children's sprints, drawn dimmer), filled by the share of points done.

var roadmapDoneStyle, roadmapTodoStyle, roadmapTodayStyle lipgloss.Style

// roadmapZooms are the days one column covers.
var roadmapZooms = []int{1, 2, 4, 7, 14}

const roadmapDefaultZoom = 1

type roadmapState struct {
	project string
	epics   []jira.Epic
	loading bool
	err     string
	fetched time.Time
	idx     int // the selected row
	top     int // the first shown row
	zoom    int // index into roadmapZooms
	from    time.Time
	open    map[string]bool // epics folded out
	// pending holds epics whose dates changed and aren't written yet;
	// saveSeq debounces the write to the last key press.
	pending map[string]bool
	saveSeq int
}

// roadmapRow is one line: an epic (kid -1) or one of its children.
type roadmapRow struct{ epic, kid int }

type roadmapMsg struct {
	project string
	epics   []jira.Epic
	err     error
}

// roadmapSaveMsg fires a pause after the last date change.
type roadmapSaveMsg struct{ seq int }

// roadmapSavedMsg is the date writes answered.
type roadmapSavedMsg struct {
	keys []string
	err  error
}

const roadmapSaveDelay = 800 * time.Millisecond

// openRoadmap swaps the board for the project's roadmap.
func (m *Model) openRoadmap() tea.Cmd {
	t := m.jiraTab
	if t.project == "" {
		return nil
	}
	r := &roadmapState{project: t.project, zoom: roadmapDefaultZoom, open: map[string]bool{}, pending: map[string]bool{}}
	r.from = roadmapStart(time.Now(), roadmapZooms[r.zoom])
	t.roadmap = r
	return m.loadRoadmap()
}

func (m *Model) loadRoadmap() tea.Cmd {
	r := m.jiraTab.roadmap
	r.loading = true
	c, ctx, project := m.jiraClient, m.ctx, r.project
	return func() tea.Msg {
		epics, err := c.Roadmap(ctx, project)
		return roadmapMsg{project: project, epics: epics, err: err}
	}
}

func (m Model) handleRoadmap(msg roadmapMsg) (tea.Model, tea.Cmd) {
	r := m.jiraTab.roadmap
	if r == nil || r.project != msg.project {
		return m, nil
	}
	r.loading = false
	if msg.err != nil {
		r.err = msg.err.Error()
		return m, nil
	}
	// Keep the selection on the same issue across a reload.
	keep := m.roadmapKey()
	r.err, r.epics, r.fetched = "", msg.epics, time.Now()
	r.idx = 0
	for i, row := range r.rows() {
		if r.rowKey(row) == keep {
			r.idx = i
		}
	}
	return m, nil
}

// rows are the shown lines: every epic, and the children of those open.
func (r *roadmapState) rows() []roadmapRow {
	var out []roadmapRow
	for i, e := range r.epics {
		out = append(out, roadmapRow{i, -1})
		if r.open[e.Key] {
			for k := range e.Kids {
				out = append(out, roadmapRow{i, k})
			}
		}
	}
	return out
}

func (r *roadmapState) rowKey(row roadmapRow) string {
	if row.kid < 0 {
		return r.epics[row.epic].Key
	}
	return r.epics[row.epic].Kids[row.kid].Key
}

// selected is the row under the cursor.
func (r *roadmapState) selected() (roadmapRow, bool) {
	rows := r.rows()
	if r.idx < 0 || r.idx >= len(rows) {
		return roadmapRow{}, false
	}
	return rows[r.idx], true
}

// roadmapKey is the selected issue's key, "" for none.
func (m *Model) roadmapKey() string {
	r := m.jiraTab.roadmap
	if row, ok := r.selected(); ok {
		return r.rowKey(row)
	}
	return ""
}

// roadmapStart is the first column's day: a little before today, on a
// Monday once a column is a week or more.
func roadmapStart(now time.Time, zoom int) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	d = d.AddDate(0, 0, -3*zoom)
	if zoom >= 7 {
		d = d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	}
	return d
}

func (m Model) handleRoadmapKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.jiraTab.roadmap
	zoom := roadmapZooms[r.zoom]
	last := max(len(r.rows())-1, 0)
	switch {
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case msg.String() == "esc", key.Matches(msg, m.keys.Roadmap):
		save := m.saveRoadmap()
		m.jiraTab.roadmap = nil
		m.renderJira()
		return m, save
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.InputUp):
		r.idx = max(r.idx-1, 0)
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.InputDown):
		r.idx = min(r.idx+1, last)
	case key.Matches(msg, m.keys.Home):
		r.idx = 0
	case key.Matches(msg, m.keys.End):
		r.idx = last
	case msg.String() == "space":
		m.foldRoadmap()
	case key.Matches(msg, m.keys.Left):
		r.from = r.from.AddDate(0, 0, -8*zoom)
	case key.Matches(msg, m.keys.Right):
		r.from = r.from.AddDate(0, 0, 8*zoom)
	case key.Matches(msg, m.keys.MoveCardLeft):
		return m, m.shiftRoadmap(-zoom, -zoom)
	case key.Matches(msg, m.keys.MoveCardRight):
		return m, m.shiftRoadmap(zoom, zoom)
	case msg.String() == "<":
		return m, m.shiftRoadmap(0, -zoom)
	case msg.String() == ">":
		return m, m.shiftRoadmap(0, zoom)
	case msg.String() == "+", msg.String() == "=":
		m.zoomRoadmap(-1)
	case msg.String() == "-":
		m.zoomRoadmap(1)
	case msg.String() == ".":
		r.from = roadmapStart(time.Now(), zoom)
	case key.Matches(msg, m.keys.Refresh):
		return m, tea.Batch(m.saveRoadmap(), m.loadRoadmap())
	case msg.String() == "f":
		row, ok := r.selected()
		if !ok {
			break
		}
		e := r.epics[row.epic]
		save := m.saveRoadmap()
		m.jiraTab.roadmap = nil
		return m, tea.Batch(save, m.runNamedJQLView("Epic: "+e.Key, "parent = "+e.Key+" ORDER BY rank"))
	case key.Matches(msg, m.keys.Create):
		m.jiraCreateParent, m.jiraCreateProject = "", ""
		m.openJiraCreateSummary("Epic")
		m.jiraCreateReload = true
	case key.Matches(msg, m.keys.OpenAttach):
		if k := m.roadmapKey(); k != "" {
			url := m.jiraClient.BrowseURL(k)
			m.status = "opening " + url + "…"
			return m, m.openOpenable(openable{name: k, url: url})
		}
	case key.Matches(msg, m.keys.OpenChannel), key.Matches(msg, m.keys.OpenRef):
		if k := m.roadmapKey(); k != "" {
			return m.openJiraKey(k)
		}
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	}
	return m, nil
}

// foldRoadmap opens or closes the selected epic; on a child it closes the
// child's epic and selects it.
func (m *Model) foldRoadmap() {
	r := m.jiraTab.roadmap
	row, ok := r.selected()
	if !ok {
		return
	}
	e := r.epics[row.epic]
	if len(e.Kids) == 0 {
		m.status = e.Key + " has no child issues"
		return
	}
	r.open[e.Key] = !r.open[e.Key]
	for i, rw := range r.rows() {
		if rw == (roadmapRow{row.epic, -1}) {
			r.idx = i
		}
	}
}

// shiftRoadmap moves the selected epic's start by ds days and its end by de,
// then writes both once the keys pause. An epic without dates gets them from
// today.
func (m *Model) shiftRoadmap(ds, de int) tea.Cmd {
	r := m.jiraTab.roadmap
	row, ok := r.selected()
	if !ok {
		return nil
	}
	if ds != 0 && !m.jiraClient.CanSetStart(m.ctx) {
		m.status = "no start date field in Jira: < > move the end"
		return nil
	}
	key, start, end, fromSprints := r.rowDates(row)
	if start.IsZero() && end.IsZero() {
		now := time.Now()
		*start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	}
	if !start.IsZero() {
		*start = start.AddDate(0, 0, ds)
	}
	e := *end
	if e.IsZero() {
		e = *start
	}
	if e = e.AddDate(0, 0, de); !start.IsZero() && e.Before(*start) {
		e = *start
	}
	*end, *fromSprints = e, false
	r.pending[key] = true
	r.saveSeq++
	m.status = fmt.Sprintf("%s %s – %s", key, roadmapDate(*start), roadmapDate(*end))
	seq := r.saveSeq
	return tea.Tick(roadmapSaveDelay, func(time.Time) tea.Msg { return roadmapSaveMsg{seq} })
}

func roadmapDate(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	return t.Format("Mon 2 Jan")
}

func (m Model) handleRoadmapSave(msg roadmapSaveMsg) (tea.Model, tea.Cmd) {
	if r := m.jiraTab.roadmap; r == nil || msg.seq != r.saveSeq {
		return m, nil
	}
	return m, m.saveRoadmap()
}

// saveRoadmap writes every pending epic's dates.
func (m *Model) saveRoadmap() tea.Cmd {
	r := m.jiraTab.roadmap
	if r == nil || len(r.pending) == 0 {
		return nil
	}
	type dates struct {
		key        string
		start, end time.Time
	}
	var todo []dates
	var keys []string
	add := func(key string, start, end time.Time) {
		if r.pending[key] {
			todo = append(todo, dates{key, start, end})
			keys = append(keys, key)
		}
	}
	for _, e := range r.epics {
		add(e.Key, e.Start, e.End)
		for _, k := range e.Kids {
			add(k.Key, k.Start, k.End)
		}
	}
	r.pending = map[string]bool{}
	c, ctx := m.jiraClient, m.ctx
	m.status = "saving " + strings.Join(keys, ", ") + "…"
	return func() tea.Msg {
		for _, d := range todo {
			if err := c.SetDates(ctx, d.key, d.start, d.end); err != nil {
				return roadmapSavedMsg{keys: keys, err: fmt.Errorf("%s: %w", d.key, err)}
			}
		}
		return roadmapSavedMsg{keys: keys}
	}
}

// handleRoadmapSaved reports the write; a failure reloads to show Jira's
// dates again.
func (m Model) handleRoadmapSaved(msg roadmapSavedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "dates not saved: " + msg.err.Error()
		if m.jiraTab.roadmap != nil {
			return m, m.loadRoadmap()
		}
		return m, nil
	}
	m.status = strings.Join(msg.keys, ", ") + " dates saved"
	return m, nil
}

// zoomRoadmap steps the zoom, keeping the selected row's start (or today)
// in the same column.
func (m *Model) zoomRoadmap(d int) {
	r := m.jiraTab.roadmap
	z := min(max(r.zoom+d, 0), len(roadmapZooms)-1)
	if z == r.zoom {
		return
	}
	anchor := time.Now()
	if row, ok := r.selected(); ok {
		if s := r.rowEpic(row).Start; !s.IsZero() {
			anchor = s
		}
	}
	col := int(anchor.Sub(r.from).Hours()/24) / roadmapZooms[r.zoom]
	r.zoom = z
	r.from = time.Date(anchor.Year(), anchor.Month(), anchor.Day()-col*roadmapZooms[z], 0, 0, 0, 0, time.Local)
}

// rowDates points at a row's key and dates, an epic's or a child's.
func (r *roadmapState) rowDates(row roadmapRow) (key string, start, end *time.Time, fromSprints *bool) {
	e := &r.epics[row.epic]
	if row.kid < 0 {
		return e.Key, &e.Start, &e.End, &e.DatesFromSprints
	}
	k := &e.Kids[row.kid]
	return k.Key, &k.Start, &k.End, &k.DatesFromSprints
}

// rowEpic is the row as a bar draws it: a child is one epic-like span, all
// done or not.
func (r *roadmapState) rowEpic(row roadmapRow) jira.Epic {
	if row.kid < 0 {
		return r.epics[row.epic]
	}
	k := r.epics[row.epic].Kids[row.kid]
	e := jira.Epic{Key: k.Key, Summary: k.Summary, Done: k.Done, Start: k.Start, End: k.End,
		DatesFromSprints: k.DatesFromSprints, Children: 1}
	if k.Done {
		e.DoneChildren = 1
	}
	return e
}

// roadmapLine is the view line while the roadmap shows.
func (m *Model) roadmapLine() string {
	r := m.jiraTab.roadmap
	s := jiraViewActive.Render("Roadmap") + jiraDimStyle.Render(fmt.Sprintf("  %d epics · %s per column", len(r.epics), roadmapZoomName(roadmapZooms[r.zoom])))
	switch {
	case r.loading:
		s += jiraDimStyle.Render("  ·  loading…")
	case !r.fetched.IsZero():
		s += jiraDimStyle.Render("  ·  updated " + age(r.fetched))
	}
	k := m.keys
	return s + jiraDimStyle.Render("  ·  ← → scroll  + - zoom  . today  space children  "+
		helpKey(k.MoveCardLeft)+"/"+helpKey(k.MoveCardRight)+" move  < > end  "+helpKey(k.OpenChannel)+" open  esc board")
}

func roadmapZoomName(days int) string {
	switch days {
	case 1:
		return "day"
	case 7:
		return "week"
	case 14:
		return "2 weeks"
	}
	return fmt.Sprintf("%d days", days)
}

// renderRoadmap draws the timeline into width × height.
func (m *Model) renderRoadmap(width, height int) string {
	r := m.jiraTab.roadmap
	switch {
	case r.err != "":
		return refErrStyle.Render(r.err)
	case len(r.epics) == 0 && r.loading:
		return refDimStyle.Render("loading…")
	case len(r.epics) == 0:
		return refDimStyle.Render("no open epics in " + r.project)
	}
	labelW := min(max(width/3, 20), 44)
	cols := max(width-labelW-1, 1)
	zoom := roadmapZooms[r.zoom]
	now := time.Now()
	today := int(now.Sub(r.from).Hours()/24) / zoom
	if now.Before(r.from) {
		today = -1
	}

	lines := []string{strings.Repeat(" ", labelW+1) + roadmapHeader(r.from, cols, zoom)}
	shown := max(height-1, 1)
	rows := r.rows()
	r.top = min(max(r.top, r.idx-shown+1), r.idx)
	for i := r.top; i < len(rows) && i < r.top+shown; i++ {
		row := rows[i]
		e := r.rowEpic(row)
		label := m.roadmapLabel(r, row, labelW)
		if i == r.idx {
			label = selectedRow.Render(ansi.Strip(label))
		} else if e.Done {
			label = jiraDimStyle.Render(ansi.Strip(label))
		}
		lines = append(lines, label+" "+roadmapBar(e, r.from, cols, zoom, today))
	}
	return strings.Join(lines, "\n")
}

// roadmapLabel is a row's left column: fold mark, key, summary and share
// done for an epic; type icon, key and summary for a child.
func (m *Model) roadmapLabel(r *roadmapState, row roadmapRow, w int) string {
	e := r.rowEpic(row)
	lead := "  "
	pct := ""
	if row.kid >= 0 {
		lead = "   " + jiraTypeIcon(r.epics[row.epic].Kids[row.kid].Type) + " "
	} else {
		if len(e.Kids) > 0 {
			lead = "▸ "
			if r.open[e.Key] {
				lead = "▾ "
			}
		}
		if f, ok := roadmapDone(e); ok {
			pct = fmt.Sprintf(" %3.0f%%", f*100)
		}
	}
	name := ansi.Truncate(lead+e.Key+" "+e.Summary, max(w-len(pct), 1), "…")
	return name + strings.Repeat(" ", max(w-lipgloss.Width(name)-len(pct), 0)) + pct
}

// roadmapDone is the epic's done share: by points, else by children.
func roadmapDone(e jira.Epic) (float64, bool) {
	switch {
	case e.Points > 0:
		return e.DonePoints / e.Points, true
	case e.Children > 0:
		return float64(e.DoneChildren) / float64(e.Children), true
	}
	return 0, false
}

// roadmapHeader marks each month's start with its name, cut short where
// the next month starts.
func roadmapHeader(from time.Time, cols, zoom int) string {
	type mark struct {
		col  int
		name string
	}
	var marks []mark
	var last time.Month // none yet: the first column gets a name
	for c := 0; c < cols; c++ {
		d := from.AddDate(0, 0, c*zoom)
		if d.Month() == last {
			continue
		}
		last = d.Month()
		name := d.Format("Jan")
		if d.Month() == time.January || c == 0 {
			name = d.Format("Jan 2006")
		}
		marks = append(marks, mark{c, name})
	}
	line := []rune(strings.Repeat(" ", cols))
	for i, mk := range marks {
		stop := cols
		if i+1 < len(marks) {
			stop = marks[i+1].col
		}
		for j, ch := range []rune("▏" + mk.name) {
			if mk.col+j < stop {
				line[mk.col+j] = ch
			}
		}
	}
	return jiraDimStyle.Render(string(line))
}

// roadmapBar is an epic's timeline row: the bar over its days, the done
// share filled from the left, today's column marked.
func roadmapBar(e jira.Epic, from time.Time, cols, zoom, today int) string {
	colOf := func(t time.Time) int { return int(math.Floor(t.Sub(from).Hours() / 24 / float64(zoom))) }
	start, end := e.Start, e.End
	if start.IsZero() && end.IsZero() {
		return jiraDimStyle.Render("no dates")
	}
	single := start.IsZero() || end.IsZero()
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}
	s, t := colOf(start), colOf(end)
	done, _ := roadmapDone(e)
	fill := s + int(math.Round(done*float64(t-s+1)))
	todo, filled := roadmapTodoStyle, roadmapDoneStyle
	if e.DatesFromSprints {
		todo, filled = todo.Faint(true), filled.Faint(true)
	}
	type cell struct {
		glyph string
		style *lipgloss.Style
	}
	blank := cell{" ", nil}
	cellAt := func(c int) cell {
		switch {
		case single && c == s:
			return cell{"◆", &todo}
		case !single && c >= s && c <= t && c < fill:
			return cell{"█", &filled}
		case !single && c >= s && c <= t:
			return cell{"▒", &todo}
		case c == today:
			return cell{"│", &roadmapTodayStyle}
		}
		return blank
	}
	// One escape per run of like cells, not per cell.
	var b strings.Builder
	for c := 0; c < cols; {
		cl := cellAt(c)
		n := 1
		for c+n < cols && cellAt(c+n) == cl {
			n++
		}
		run := strings.Repeat(cl.glyph, n)
		if cl.style != nil {
			run = cl.style.Render(run)
		}
		b.WriteString(run)
		c += n
	}
	return b.String()
}
