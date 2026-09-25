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
	idx     int // the selected epic
	top     int // the first shown row
	zoom    int // index into roadmapZooms
	from    time.Time
}

type roadmapMsg struct {
	project string
	epics   []jira.Epic
	err     error
}

// openRoadmap swaps the board for the project's roadmap.
func (m *Model) openRoadmap() tea.Cmd {
	t := m.jiraTab
	if t.project == "" {
		return nil
	}
	r := &roadmapState{project: t.project, zoom: roadmapDefaultZoom}
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
	r.err, r.epics, r.fetched = "", msg.epics, time.Now()
	r.idx = min(r.idx, max(len(r.epics)-1, 0))
	return m, nil
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
	switch {
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case msg.String() == "esc", key.Matches(msg, m.keys.Roadmap):
		m.jiraTab.roadmap = nil
		m.renderJira()
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.InputUp):
		r.idx = max(r.idx-1, 0)
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.InputDown):
		r.idx = min(r.idx+1, max(len(r.epics)-1, 0))
	case key.Matches(msg, m.keys.Home):
		r.idx = 0
	case key.Matches(msg, m.keys.End):
		r.idx = max(len(r.epics)-1, 0)
	case key.Matches(msg, m.keys.Left):
		r.from = r.from.AddDate(0, 0, -8*zoom)
	case key.Matches(msg, m.keys.Right):
		r.from = r.from.AddDate(0, 0, 8*zoom)
	case msg.String() == "+", msg.String() == "=":
		m.zoomRoadmap(-1)
	case msg.String() == "-":
		m.zoomRoadmap(1)
	case msg.String() == ".":
		r.from = roadmapStart(time.Now(), zoom)
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadRoadmap()
	case key.Matches(msg, m.keys.OpenAttach):
		if r.idx < len(r.epics) {
			url := m.jiraClient.BrowseURL(r.epics[r.idx].Key)
			m.status = "opening " + url + "…"
			return m, m.openOpenable(openable{name: r.epics[r.idx].Key, url: url})
		}
	case key.Matches(msg, m.keys.OpenChannel), key.Matches(msg, m.keys.OpenRef):
		if r.idx < len(r.epics) {
			return m.openJiraKey(r.epics[r.idx].Key)
		}
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	}
	return m, nil
}

// zoomRoadmap steps the zoom, keeping the selected epic's start (or today)
// in the same column.
func (m *Model) zoomRoadmap(d int) {
	r := m.jiraTab.roadmap
	z := min(max(r.zoom+d, 0), len(roadmapZooms)-1)
	if z == r.zoom {
		return
	}
	anchor := time.Now()
	if r.idx < len(r.epics) && !r.epics[r.idx].Start.IsZero() {
		anchor = r.epics[r.idx].Start
	}
	col := int(anchor.Sub(r.from).Hours()/24) / roadmapZooms[r.zoom]
	r.zoom = z
	r.from = time.Date(anchor.Year(), anchor.Month(), anchor.Day()-col*roadmapZooms[z], 0, 0, 0, 0, time.Local)
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
	return s + jiraDimStyle.Render("  ·  ← → scroll  + - zoom  . today  "+helpKey(m.keys.OpenChannel)+" open  esc board")
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
	rows := max(height-1, 1)
	r.top = min(max(r.top, r.idx-rows+1), r.idx)
	for i := r.top; i < len(r.epics) && i < r.top+rows; i++ {
		e := r.epics[i]
		label := roadmapLabel(e, labelW)
		if i == r.idx {
			label = selectedRow.Render(label)
		} else if e.Done {
			label = jiraDimStyle.Render(label)
		}
		lines = append(lines, label+" "+roadmapBar(e, r.from, cols, zoom, today))
	}
	return strings.Join(lines, "\n")
}

// roadmapLabel is an epic's left column: key, summary, share done.
func roadmapLabel(e jira.Epic, w int) string {
	pct := ""
	if f, ok := roadmapDone(e); ok {
		pct = fmt.Sprintf(" %3.0f%%", f*100)
	}
	name := ansi.Truncate(e.Key+" "+e.Summary, max(w-len(pct), 1), "…")
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
