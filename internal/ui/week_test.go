package ui

import (
	"slices"
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

// TestWeekAddRow: # adds an issue's row to log on, the cursor on it in
// today's column; today isn't counted short.
func TestWeekAddRow(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.week = &weekState{from: weekStart(time.Now())}
	out, _ := m.Update(weekMsg{seq: m.jiraTab.week.seq})
	m = out.(Model)
	out, _ = m.handleKey(keyMsg(t, "#"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "week-add" {
		t.Fatalf("#: field %q active %v", m.jiraFieldName, m.jiraFieldActive)
	}
	m.jiraFieldInput.SetValue("3")
	out, _ = m.handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	w := m.jiraTab.week
	today := (int(time.Now().Weekday()) + 6) % 7
	if len(w.rows) != 1 || w.rows[0].key != "ABC-3" || w.row != 0 || w.col != today {
		t.Fatalf("rows %v, cursor %d,%d", w.rows, w.row, w.col)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "ABC-3 Third") {
		t.Errorf("no row:\n%s", view)
	}
	if lines := strings.Split(view, "\n"); slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "Short of 8h") && strings.Count(l, "-8h") > today
	}) {
		t.Errorf("today counted short:\n%s", view)
	}
	out, _ = m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); m.jiraFieldKey != "ABC-3" || m.jiraFieldName != "worklog" {
		t.Errorf("enter on the new row: logging on %q", m.jiraFieldKey)
	}
}
