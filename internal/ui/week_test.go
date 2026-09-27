package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestWeek: W in the timesheet opens the week grid with day and week
// totals and the gap to 8h; enter on a cell logs on that day.
func TestWeek(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "W"))
	m = out.(Model)
	out, cmd := m.handleJiraPickerKey(keyMsg(t, "W"))
	m = out.(Model)
	if m.jiraPicker.active || m.jiraTab.week == nil || cmd == nil {
		t.Fatal("W in the timesheet should open the week")
	}
	w := m.jiraTab.week
	if w.from.Weekday() != time.Monday || time.Since(w.from) > 7*24*time.Hour {
		t.Fatalf("week from %v", w.from)
	}
	w.from, w.col = time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local), 0 // a past week, all its gaps show
	at := func(d, h int) time.Time { return w.from.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour) }
	out, _ = m.Update(weekMsg{seq: w.seq, logs: []jira.Worklog{
		{Key: "ABC-10", Summary: "Tenth", Seconds: 3600, Started: at(0, 9)},
		{Key: "ABC-2", Summary: "Second", Seconds: 5 * 3600, Started: at(0, 10)},
		{Key: "ABC-2", Summary: "Second", Seconds: 2 * 3600, Started: at(1, 9)},
		{Key: "ABC-2", Summary: "Second", Seconds: 1800, Started: at(0, 16)},
	}})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Week of Mon 21 Sep  ·  8h 30m", "Mon 21", "ABC-2 Second", "5h 30m      2h", "ABC-10 Tenth", "Short of 8h", "-1h 30m", "-6h"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	if w.rows[0].key != "ABC-2" {
		t.Errorf("rows by key: %v", w.rows)
	}
	if !strings.Contains(m.weekTable(), "| ABC-2 | Second | 5h 30m | 2h | · |") {
		t.Errorf("table:\n%s", m.weekTable())
	}
	out, _ = m.handleJiraKey(keyMsg(t, "right"))
	m = out.(Model)
	out, _ = m.handleJiraKey(keyMsg(t, "enter"))
	m = out.(Model)
	if m.jiraFieldKey != "ABC-2" || !m.worklogStart.Equal(at(1, 9)) {
		t.Errorf("log on %q at %v", m.jiraFieldKey, m.worklogStart)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "what you did") {
		t.Errorf("the worklog input should show over the week:\n%s", view)
	}
	m.closeJiraField()
	out, _ = m.handleJiraKey(keyMsg(t, "esc"))
	if m = out.(Model); m.jiraTab.week != nil {
		t.Error("esc should close the week")
	}
}
