package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
