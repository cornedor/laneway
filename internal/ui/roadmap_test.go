package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// roadmapModel is the board with its roadmap open on two epics.
func roadmapModel(t *testing.T) Model {
	t.Helper()
	m := jiraTabModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "R"))
	m = out.(Model)
	if m.jiraTab.roadmap == nil || cmd == nil {
		t.Fatal("R should open the roadmap and load it")
	}
	today := time.Now()
	day := func(d int) time.Time {
		return time.Date(today.Year(), today.Month(), today.Day()+d, 0, 0, 0, 0, time.Local)
	}
	out, _ = m.handleRoadmap(roadmapMsg{project: "ABC", epics: []jira.Epic{
		{Key: "ABC-10", Summary: "Checkout", Start: day(-4), End: day(10), Points: 10, DonePoints: 5,
			Kids: []jira.EpicChild{{Key: "ABC-12", Summary: "Pay", Type: "Story", Done: true, End: day(2)}}},
		{Key: "ABC-11", Summary: "Search"},
	}})
	return out.(Model)
}

func TestRoadmapView(t *testing.T) {
	m := roadmapModel(t)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Roadmap", "2 epics", "ABC-10 Checkout", "50%", "█", "▒", "no dates"} {
		if !strings.Contains(view, want) {
			t.Errorf("roadmap lacks %q", want)
		}
	}
	if strings.Contains(view, "To do 2") {
		t.Error("lanes should give way to the roadmap")
	}
	out, _ := m.handleJiraKey(keyMsg(t, "esc"))
	if m = out.(Model); m.jiraTab.roadmap != nil || !strings.Contains(m.View().Content, "To do 2") {
		t.Error("esc should bring the board back")
	}
}

// TestRoadmapKeys: j moves, + / - zoom, enter opens the epic in the panel.
func TestRoadmapKeys(t *testing.T) {
	m := roadmapModel(t)
	r := m.jiraTab.roadmap
	step := func(k string) {
		t.Helper()
		out, _ := m.handleJiraKey(keyMsg(t, k))
		m = out.(Model)
	}
	step("j")
	if r.idx != 1 {
		t.Fatalf("j: idx %d", r.idx)
	}
	step("k")
	step("-")
	if roadmapZooms[r.zoom] != 4 {
		t.Fatalf("zoom = %d days", roadmapZooms[r.zoom])
	}
	// Zooming keeps the selected epic's start in its column.
	col := int(r.epics[0].Start.Sub(r.from).Hours()/24) / roadmapZooms[r.zoom]
	step("+")
	if got := int(r.epics[0].Start.Sub(r.from).Hours()/24) / roadmapZooms[r.zoom]; got != col {
		t.Errorf("start column %d → %d", col, got)
	}
	from := r.from
	step("l")
	if !r.from.After(from) {
		t.Error("l should scroll later")
	}
	out, cmd := m.handleJiraKey(keyMsg(t, "enter"))
	if m = out.(Model); !m.refOpen || cmd == nil || m.refs[m.refIdx].jiraKey != "ABC-10" {
		t.Fatal("enter should open the epic in the panel")
	}
}

func TestRoadmapBar(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	e := jira.Epic{Start: from.AddDate(0, 0, 2), End: from.AddDate(0, 0, 5), Children: 2, DoneChildren: 1}
	if got := ansi.Strip(roadmapBar(e, from, 10, 1, 8)); got != "  ██▒▒    " && got != "  ██▒▒  │ " {
		t.Errorf("bar = %q", got)
	}
	e = jira.Epic{End: from.AddDate(0, 0, 3)}
	if got := ansi.Strip(roadmapBar(e, from, 6, 1, -1)); got != "   ◆  " {
		t.Errorf("milestone = %q", got)
	}
}

func TestRoadmapHeader(t *testing.T) {
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.Local)
	if got := ansi.Strip(roadmapHeader(from.AddDate(0, 0, -20), 30, 1)); got != "▏Sep 2026             ▏Oct    " {
		t.Errorf("header = %q", got)
	}
	if got := ansi.Strip(roadmapHeader(from, 20, 1)); got != "▏S▏Oct              " {
		t.Errorf("header = %q", got)
	}
}

// fakeRoadmapJira serves the field list (with a start field when withStart)
// and records date writes.
func fakeRoadmapJira(t *testing.T, m *Model, withStart bool, writes *[]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			if withStart {
				io.WriteString(w, `[{"id":"customfield_20","name":"Start date"}]`)
			} else {
				io.WriteString(w, `[]`)
			}
			return
		}
		b, _ := io.ReadAll(r.Body)
		*writes = append(*writes, r.URL.Path+" "+string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
}

// TestRoadmapFold: space opens an epic's children under it, and on a child
// folds back to the epic.
func TestRoadmapFold(t *testing.T) {
	m := roadmapModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "space"))
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "▾ ABC-10") || !strings.Contains(view, "ABC-12 Pay") {
		t.Fatal("children not shown")
	}
	out, _ = m.handleJiraKey(keyMsg(t, "j"))
	m = out.(Model)
	if m.roadmapKey() != "ABC-12" {
		t.Fatalf("j onto child: %q", m.roadmapKey())
	}
	out, _ = m.handleJiraKey(keyMsg(t, "space"))
	m = out.(Model)
	if m.roadmapKey() != "ABC-10" || strings.Contains(m.View().Content, "ABC-12") {
		t.Error("space on a child should fold to its epic")
	}
}

// TestRoadmapMove: L shifts the bar, > stretches the end; one write after
// the keys pause, with both dates.
func TestRoadmapMove(t *testing.T) {
	m := roadmapModel(t)
	var writes []string
	fakeRoadmapJira(t, &m, true, &writes)
	e := &m.jiraTab.roadmap.epics[0]
	start, end := e.Start, e.End
	var ticks []tea.Cmd
	for _, k := range []string{"L", "L", ">"} {
		out, cmd := m.handleJiraKey(keyMsg(t, k))
		m, ticks = out.(Model), append(ticks, cmd)
	}
	zoom := roadmapZooms[m.jiraTab.roadmap.zoom]
	if !e.Start.Equal(start.AddDate(0, 0, 2*zoom)) || !e.End.Equal(end.AddDate(0, 0, 3*zoom)) {
		t.Fatalf("dates %v – %v", e.Start, e.End)
	}
	// Only the last tick saves.
	if out, cmd := m.handleRoadmapSave(ticks[0]().(roadmapSaveMsg)); cmd != nil {
		t.Fatal("a stale tick should not save")
	} else {
		m = out.(Model)
	}
	_, cmd := m.handleRoadmapSave(ticks[2]().(roadmapSaveMsg))
	if cmd == nil {
		t.Fatal("expected a save")
	}
	if msg := cmd().(roadmapSavedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	want := `/rest/api/3/issue/ABC-10 {"fields":{"customfield_20":"` + e.Start.Format(time.DateOnly) + `","duedate":"` + e.End.Format(time.DateOnly) + `"}}`
	if len(writes) != 1 || writes[0] != want {
		t.Errorf("writes = %q, want %q", writes, want)
	}
}

// TestRoadmapNoStartField: without a start field only the end moves.
func TestRoadmapNoStartField(t *testing.T) {
	m := roadmapModel(t)
	var writes []string
	fakeRoadmapJira(t, &m, false, &writes)
	out, cmd := m.handleJiraKey(keyMsg(t, "L"))
	if m = out.(Model); cmd != nil || !strings.Contains(m.status, "no start date field") {
		t.Fatalf("L: status %q", m.status)
	}
	if _, cmd = m.handleJiraKey(keyMsg(t, ">")); cmd == nil {
		t.Error("> should still move the end")
	}
}

// TestRoadmapChildMove: a child's bar moves and saves like an epic's.
func TestRoadmapChildMove(t *testing.T) {
	m := roadmapModel(t)
	var writes []string
	fakeRoadmapJira(t, &m, true, &writes)
	for _, k := range []string{"space", "j", ">"} {
		out, _ := m.handleJiraKey(keyMsg(t, k))
		m = out.(Model)
	}
	cmd := m.saveRoadmap()
	cmd()
	if len(writes) != 1 || !strings.HasPrefix(writes[0], "/rest/api/3/issue/ABC-12 ") {
		t.Errorf("writes = %q", writes)
	}
}

// TestRoadmapFilterAndNew: f shows the epic's issues as a view; n asks for
// a new epic.
func TestRoadmapFilterAndNew(t *testing.T) {
	m := roadmapModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "f"))
	m = out.(Model)
	v := m.jiraTab.views[len(m.jiraTab.views)-1]
	if m.jiraTab.roadmap != nil || cmd == nil || v.name != "Epic: ABC-10" || v.jql != "parent = ABC-10 ORDER BY rank" {
		t.Fatalf("view = %+v", v)
	}
	m = roadmapModel(t)
	out, _ = m.handleJiraKey(keyMsg(t, "n"))
	if m = out.(Model); !m.jiraCreateActive || m.jiraCreateType != "Epic" || !m.jiraCreateReload {
		t.Errorf("create: active %v type %q", m.jiraCreateActive, m.jiraCreateType)
	}
}

// TestRoadmapBlocked: an open blocker ending after the epic starts is a
// conflict; one ending before is fine; a done one does not count.
func TestRoadmapBlocked(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.Local) }
	r := &roadmapState{epics: []jira.Epic{
		{Key: "E-1", End: day(10)},
		{Key: "E-2", Start: day(5), BlockedBy: []string{"E-1"}},
		{Key: "E-3", Start: day(12), BlockedBy: []string{"E-1"}},
		{Key: "E-4", Done: true, End: day(20)},
		{Key: "E-5", Start: day(1), BlockedBy: []string{"E-4"}},
	}}
	for i, want := range map[int]int{1: blockConflict, 2: blockOK, 4: blockNone} {
		if got := roadmapBlock(r, r.epics[i]); got != want {
			t.Errorf("%s: %d, want %d", r.epics[i].Key, got, want)
		}
	}
}

// TestRoadmapParents: epics sit under their parent, whose bar spans them;
// space folds the parent, and its dates don't move.
func TestRoadmapParents(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "R"))
	m = out.(Model)
	d := func(n int) time.Time { return time.Now().Truncate(24*time.Hour).AddDate(0, 0, n) }
	out, _ = m.handleRoadmap(roadmapMsg{project: "ABC", epics: []jira.Epic{
		{Key: "ABC-10", Summary: "Checkout", Parent: "ABC-1", ParentSummary: "Grow", Start: d(0), End: d(5), Children: 2, DoneChildren: 1},
		{Key: "ABC-11", Summary: "Search"},
		{Key: "ABC-12", Summary: "Pay", Parent: "ABC-1", ParentSummary: "Grow", Start: d(3), End: d(9), Children: 2, DoneChildren: 2},
	}})
	m = out.(Model)
	r := m.jiraTab.roadmap
	var keys []string
	for _, row := range r.rows() {
		keys = append(keys, r.rowKey(row))
	}
	if got := strings.Join(keys, " "); got != "ABC-11 ABC-1 ABC-10 ABC-12" {
		t.Fatalf("rows = %s", got)
	}
	g := r.groupEpic(r.groups[0])
	if !g.Start.Equal(d(0)) || !g.End.Equal(d(9)) || g.DoneChildren != 3 || g.Done {
		t.Errorf("parent bar = %+v", g)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"1 parents", "▾ ABC-1 Grow", " 75%", "    ABC-10 Checkout"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	r.idx = 1
	out, cmd := m.handleJiraKey(keyMsg(t, "L"))
	if m = out.(Model); cmd != nil || !strings.Contains(m.status, "parent spans its epics") {
		t.Errorf("moving a parent: %q", m.status)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "space"))
	if m = out.(Model); len(m.jiraTab.roadmap.rows()) != 2 {
		t.Error("space should fold the parent in")
	}
}

// TestRoadmapCopy: y copies the epics, dates and done points as a table.
func TestRoadmapCopy(t *testing.T) {
	m := roadmapModel(t)
	out, cmd := m.handleKey(keyMsg(t, "y"))
	m = out.(Model)
	if cmd == nil || m.status != "copied 2 epics as a markdown table" {
		t.Fatalf("status %q", m.status)
	}
	got := fmt.Sprint(cmd())
	start := time.Now().AddDate(0, 0, -4).Format(time.DateOnly)
	if !strings.HasPrefix(got, "| Epic | Summary | Status | Start | End | Done |\n") ||
		!strings.Contains(got, "/browse/ABC-10) | Checkout |  | "+start) || !strings.Contains(got, "| 5/10p |") ||
		!strings.Contains(got, "/browse/ABC-11) | Search |  |  |  |  |") {
		t.Errorf("table = %q", got)
	}
}

// TestRoadmapDrag: dragging a bar moves both dates a column's worth per
// column; dragging its end stretches only the end; the release lets go.
func TestRoadmapDrag(t *testing.T) {
	m := roadmapModel(t)
	var writes []string
	fakeRoadmapJira(t, &m, true, &writes)
	r := m.jiraTab.roadmap
	e := &r.epics[0]
	start, end := e.Start, e.End
	zoom := roadmapZooms[r.zoom]
	s, last, _ := roadmapBarCols(*e, r.from, zoom)
	labelW, _ := roadmapLayout(m.jiraTab.view.Width())
	x := func(col int) int { return 1 + labelW + 1 + col }
	y := jiraBodyTop + 1
	press := func(col int) {
		out, _ := m.Update(tea.MouseClickMsg{X: x(col), Y: y, Button: tea.MouseLeft})
		m = out.(Model)
	}
	motion := func(col int) tea.Cmd {
		out, cmd := m.Update(tea.MouseMotionMsg{X: x(col), Y: y, Button: tea.MouseLeft})
		m = out.(Model)
		return cmd
	}
	release := func() {
		out, _ := m.Update(tea.MouseReleaseMsg{X: 0, Y: y, Button: tea.MouseLeft})
		m = out.(Model)
	}

	mid := (s + last) / 2
	press(mid)
	if motion(mid+2) == nil {
		t.Fatal("a drag should schedule the write")
	}
	release()
	if !e.Start.Equal(start.AddDate(0, 0, 2*zoom)) || !e.End.Equal(end.AddDate(0, 0, 2*zoom)) {
		t.Fatalf("moved to %v – %v", e.Start, e.End)
	}
	if motion(mid+5) != nil {
		t.Error("after the release a motion should do nothing")
	}

	start, end = e.Start, e.End
	_, last, _ = roadmapBarCols(*e, r.from, zoom)
	press(last)
	motion(last + 1)
	release()
	if !e.Start.Equal(start) || !e.End.Equal(end.AddDate(0, 0, zoom)) {
		t.Errorf("stretched to %v – %v", e.Start, e.End)
	}
}

// TestRoadmapSelectedRowRunsOn: the selected row's timeline carries the
// quiet selection background to the row's end; other rows don't.
func TestRoadmapSelectedRowRunsOn(t *testing.T) {
	m := roadmapModel(t)
	lines := strings.Split(m.renderRoadmap(m.jiraTab.view.Width(), m.jiraTab.view.Height()), "\n")
	open := ansiOpenSeq(diffTreeSelStyle)
	if open == "" {
		t.Skip("no colour in this terminal profile")
	}
	if !strings.Contains(lines[1], open) {
		t.Errorf("selected row's timeline lacks the selection background: %q", lines[1])
	}
	if strings.Contains(lines[2], open) {
		t.Errorf("another row is painted: %q", lines[2])
	}
}

// TestRoadmapTabToPanel: with an issue open beside the roadmap, tab walks
// out of the panel onto the roadmap and back again.
func TestRoadmapTabToPanel(t *testing.T) {
	m := roadmapModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.refOpen {
		t.Fatal("enter should open the row's issue")
	}
	m.focus = focusJira
	out, _ = m.handleKey(keyMsg(t, "tab"))
	if m = out.(Model); m.focus != focusRef {
		t.Fatalf("tab on the roadmap: focus %v, want the panel", m.focus)
	}
}

// TestRoadmapEpicType: n on the roadmap creates ui.roadmap_epic_type.
func TestRoadmapEpicType(t *testing.T) {
	m := roadmapModel(t)
	m.opts.epicType = "Initiative"
	out, _ := m.handleKey(keyMsg(t, "n"))
	if m = out.(Model); !m.jiraCreateActive || m.jiraCreateType != "Initiative" {
		t.Errorf("create %v type %q", m.jiraCreateActive, m.jiraCreateType)
	}
	o, warn := optionsFrom(config.UIConfig{RoadmapEpicType: "Initiative", RoadmapDoneDays: 30})
	if o.epicType != "Initiative" || o.roadmapDoneDays != 30 || len(warn) != 0 {
		t.Errorf("options %q %d %v", o.epicType, o.roadmapDoneDays, warn)
	}
}

// TestRoadmapGrip: e picks up the bar's start, then its end; h/l move the
// one held, and the start stops at the end.
func TestRoadmapGrip(t *testing.T) {
	m := roadmapModel(t)
	var writes []string
	fakeRoadmapJira(t, &m, true, &writes)
	e := &m.jiraTab.roadmap.epics[0]
	start, end := e.Start, e.End
	from := m.jiraTab.roadmap.from
	press := func(keys ...string) {
		for _, k := range keys {
			out, _ := m.handleJiraKey(keyMsg(t, k))
			m = out.(Model)
		}
	}
	zoom := roadmapZooms[m.jiraTab.roadmap.zoom]
	press("e", "l")
	if !e.Start.Equal(start.AddDate(0, 0, zoom)) || !e.End.Equal(end) || !m.jiraTab.roadmap.from.Equal(from) {
		t.Fatalf("start grip: %v – %v", e.Start, e.End)
	}
	press("e", "h")
	if !e.End.Equal(end.AddDate(0, 0, -zoom)) {
		t.Fatalf("end grip: %v", e.End)
	}
	press("e")
	if m.jiraTab.roadmap.grip != "" {
		t.Fatal("a third e should let go")
	}
	press("l") // scrolls again
	if m.jiraTab.roadmap.from.Equal(from) {
		t.Error("l no longer scrolls")
	}
	press("e")
	for range 400 / max(zoom, 1) {
		press("l")
	}
	if e.Start.After(e.End) {
		t.Errorf("start passed the end: %v – %v", e.Start, e.End)
	}
}
