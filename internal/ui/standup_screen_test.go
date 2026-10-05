package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/standup"
)

// TestStandupRound: U opens on Everyone, the board walked with what was
// done on each card; → goes to each person in turn, starting the timer and
// ticking off the one left, a person without activity says so, and the
// round comes back to Everyone; esc goes back to the board.
func TestStandupRound(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"a1"}`)
		case "/rest/api/3/search/jql":
			io.WriteString(w, `{"issues":[{"key":"ABC-1","fields":{"summary":"First"}}]}`)
		case "/rest/api/3/issue/ABC-1/worklog":
			io.WriteString(w, `{"worklogs":[{"author":{"accountId":"a1","displayName":"Ada"},"started":"`+
				now.Add(-time.Minute).Format("2006-01-02T15:04:05.000-0700")+`","timeSpentSeconds":7200}]}`)
		case "/rest/api/3/issue/ABC-1/changelog":
			io.WriteString(w, `{"total":0,"values":[]}`)
		default:
			io.WriteString(w, `{"comments":[]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.jiraTab.cards = append(m.jiraTab.cards, jira.Card{Key: "ABC-5", Summary: "Fifth", StatusID: "3", InProgress: true, Assignee: "Bo Ek", AssigneeID: "b2"})
	m.buildJiraLanes()
	press := func(k tea.KeyPressMsg) {
		t.Helper()
		out, cmd := m.handleKey(k)
		m = out.(Model)
		if msg, ok := findMsg[standupMsg](cmd); ok {
			out, _ = m.Update(msg)
			m = out.(Model)
		}
	}
	press(keyStr("U"))
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Standup · since", "Everyone", "To do (1)", "ABC-1 First", "Ada", "logged 2h", "starts the timer"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	s := m.jiraTab.standup
	if len(s.stops) != 3 || s.stops[1].Person.Name != "Bo Ek" || s.stops[2].Person.Name != "Ada" {
		t.Fatalf("stops in walk order: %+v", s.stops)
	}
	next := func() { // its cmd is the timer's tick
		t.Helper()
		out, _ := m.handleKey(keyMsg(t, "right"))
		m = out.(Model)
	}
	next()
	if s.at != 1 || s.started.IsZero() || s.turn.IsZero() {
		t.Errorf("→ to Bo: at %d, started %v", s.at, s.started)
	}
	view = ansi.Strip(m.View().Content)
	for _, want := range []string{"Bo Ek · no changes", "ABC-5 Fifth", "7:30 left · 0:00 of 15:00"} {
		if !strings.Contains(view, want) {
			t.Errorf("Bo's stop: no %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "ABC-1 First") {
		t.Errorf("Bo's stop shows Ada's card:\n%s", view)
	}
	next()
	if !s.heard["b2"] || !strings.Contains(s.copyText(), "Ada\n\nTo do\n- ABC-1 First · Ada · logged 2h") {
		t.Errorf("Ada's stop: heard %v, copy %q", s.heard, s.copyText())
	}
	next()
	if s.at != 0 || !s.turn.IsZero() {
		t.Errorf("the round ends on Everyone: at %d", s.at)
	}
	press(keyStr("esc"))
	if m.jiraTab.standup != nil {
		t.Error("esc left the standup open")
	}
}

// TestStandupTimer: each turn is the length split, counts down and goes
// over; pausing leaves the pause out; shuffling keeps Everyone first and
// the stop shown.
func TestStandupTimer(t *testing.T) {
	t0 := time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)
	s := &standupState{started: t0, turn: t0.Add(time.Minute)}
	if total, left, in := s.clock(t0.Add(2*time.Minute), 3*time.Minute); total != 2*time.Minute || left != 2*time.Minute || !in {
		t.Errorf("running: %v %v %v", total, left, in)
	}
	if _, left, _ := s.clock(t0.Add(5*time.Minute), 3*time.Minute); left != -time.Minute || standupClock(-left) != "1:00" {
		t.Errorf("over: %v", left)
	}
	s.pause(t0.Add(2 * time.Minute))
	s.pause(t0.Add(12 * time.Minute))
	if total, left, _ := s.clock(t0.Add(12*time.Minute), 3*time.Minute); total != 2*time.Minute || left != 2*time.Minute {
		t.Errorf("after a pause: %v %v", total, left)
	}
	if got := standup.Defaults.Turn(4); got != 225*time.Second {
		t.Errorf("turn of 4: %v", got)
	}
	people := []standup.Stop{{}, {Person: standup.Person{ID: "a"}}, {Person: standup.Person{ID: "b"}}, {Person: standup.Person{ID: "c"}}, {Person: standup.Person{ID: "d"}}}
	s = &standupState{board: people, shuf: true}
	s.order("c", true)
	if s.stops[0].Person.ID != "" || s.stops[s.at].Person.ID != "c" || len(s.stops) != 5 {
		t.Errorf("shuffled: %+v at %d", s.stops, s.at)
	}
	gone := slices.Clone(s.stops)
	s.order("c", false)
	if !slices.EqualFunc(gone, s.stops, func(a, b standup.Stop) bool { return a.Person.ID == b.Person.ID }) {
		t.Errorf("a reload reshuffled: %+v, was %+v", s.stops, gone)
	}
}

// TestStandupPark: P parks the card, which then also shows in Everyone's
// parking lot at the end and is kept for the sprint.
func TestStandupPark(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.standup = &standupState{seq: 1, heard: map[string]bool{}}
	rows := []standup.Row{{Head: "In progress (2)"}, {Key: "ABC-1", Title: "ABC-1 One", Who: "Ann"}, {Key: "ABC-2", Title: "ABC-2 Two", What: "commented"}}
	stops := []standup.Stop{{Rows: rows, Text: "In progress\n- ABC-1 One\n- ABC-2 Two"}}
	out, _ := m.handleStandup(standupMsg{seq: 1, stops: stops})
	m = out.(Model)
	out, _ = m.handleStandupKey(keyMsg(t, "down"))
	m = out.(Model)
	out, _ = m.handleStandupKey(keyStr("P"))
	m = out.(Model)
	s := m.jiraTab.standup
	var labels []string
	for _, l := range s.lines {
		labels = append(labels, l.Head+l.Text())
	}
	want := "In progress (2)\nABC-1 One · Ann\nABC-2 Two · parked · commented\nParking lot (1)\nABC-2 Two · commented"
	if got := strings.Join(labels, "\n"); got != want {
		t.Fatalf("rows:\n%s\nwant\n%s", got, want)
	}
	if !strings.HasSuffix(s.copyText(), "\n\nParking lot\n- ABC-2 Two · commented") {
		t.Errorf("copy:\n%s", s.copyText())
	}
	// A reload keeps it: the parking lot is stored per sprint.
	out, _ = m.handleStandup(standupMsg{seq: 1, stops: stops})
	m = out.(Model)
	if s := m.jiraTab.standup; len(s.lines) != 5 || s.lines[3].Head != "Parking lot (1)" {
		t.Errorf("after a reload: %+v", s.lines)
	}
	m.jiraTab.standup.row = 2
	out, _ = m.handleStandupKey(keyStr("P"))
	m = out.(Model)
	if s := m.jiraTab.standup; len(s.lines) != 3 || strings.Contains(s.lines[2].Marks, "parked") {
		t.Errorf("unparked: %+v", s.lines)
	}
}
