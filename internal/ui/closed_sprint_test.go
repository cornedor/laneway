package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestClosedSprint: ctrl+o lists the closed sprints; picking one adds its
// view with the lanes as they closed and what carried over; writes wait,
// and esc goes back to the view it came from, dropping it.
func TestClosedSprint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"isLast": true, "values": [{"id": 42, "name": "Sprint 0", "state": "closed",
			"endDate": "2026-09-14T15:00:00.000Z", "completeDate": "2026-09-15T09:00:00.000Z", "goal": "Ship"}]}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})

	out, cmd := m.handleJiraKey(keyMsg(t, "ctrl+o"))
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if len(m.jiraPicker.items) != 1 || !strings.Contains(m.jiraPicker.items[0].label, "closed Sep 15 2026") {
		t.Fatalf("picker = %+v", m.jiraPicker.items)
	}
	m.closeJiraPicker()
	_ = m.openClosedSprint("42")
	idx := len(m.jiraTab.views) - 1
	closedAt := m.jiraTab.views[idx].closed
	if idx != 2 || closedAt.Day() != 15 {
		t.Fatalf("views = %+v", m.jiraTab.views)
	}
	out, cmd = m.handleJiraCards(jiraCardsMsg{seq: m.jiraTab.seq, viewIdx: idx, cards: []jira.Card{
		{Key: "ABC-7", Summary: "Shipped", StatusID: "5", Done: true},
		{Key: "ABC-8", Summary: "Late", StatusID: "5", Done: true, Sprint: "Sprint 1"},
		{Key: "ABC-9", Summary: "Dropped", StatusID: "1"},
	}, total: 3})
	m = out.(Model)
	if m.jiraTab.past == nil || !m.jiraTab.past.at.Equal(closedAt) || cmd == nil {
		t.Fatal("no replay pinned at the close")
	}
	// ABC-8 was still in progress when the sprint closed.
	after := closedAt.Add(24 * time.Hour)
	out, _ = m.handleTimeMachine(timeMachineMsg{seq: m.jiraTab.past.seq, moves: map[string][]jira.StatusMove{
		"ABC-8": {{When: closedAt.Add(-time.Hour), From: "1", To: "3"}, {When: after, From: "3", To: "5"}},
	}})
	m = out.(Model)
	lanes := make([]int, len(m.jiraTab.lanes))
	for i, l := range m.jiraTab.lanes {
		lanes[i] = len(l.cards)
	}
	if len(lanes) != 3 || lanes[0] != 1 || lanes[1] != 1 || lanes[2] != 1 {
		t.Errorf("lanes at the close = %v", lanes)
	}
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"as it closed", "closed Sep 15", "1 done · 2 carried over: 1 → Sprint 1, 1 → backlog"} {
		if !strings.Contains(screen, want) {
			t.Errorf("no %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "2/3") {
		t.Error("the sprint bar counts what is done now")
	}
	out, _ = m.handleJiraKey(keyMsg(t, "H"))
	if m = out.(Model); !strings.Contains(m.status, "only looks") {
		t.Errorf("H in a closed sprint: %q", m.status)
	}
	out, cmd = m.handleJiraKey(keyMsg(t, "esc"))
	m = out.(Model)
	if m.jiraTab.past != nil || cmd == nil {
		t.Fatal("esc kept the replay")
	}
	out, _ = m.handleJiraCards(jiraCardsMsg{seq: m.jiraTab.seq, viewIdx: 0, cards: m.jiraTab.cards, total: 3})
	if m = out.(Model); len(m.jiraTab.views) != 2 || m.jiraTab.viewIdx != 0 {
		t.Errorf("after esc: views %d, on %d", len(m.jiraTab.views), m.jiraTab.viewIdx)
	}
}
