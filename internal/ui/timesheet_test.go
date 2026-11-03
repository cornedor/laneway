package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/timesheet/httpsheet"
	"github.com/cornedor/laneway/internal/timesheet/sheettest"
)

// sheetModel is the board logging time to a fake timesheet server that
// runs the timer, its caps read as the first tick would.
func sheetModel(t *testing.T) (Model, *sheettest.Server) {
	t.Helper()
	srv := sheettest.New()
	t.Cleanup(srv.Close)
	sh := httpsheet.New(srv.URL, "")
	if _, err := sh.Running(context.Background()); err != nil {
		t.Fatal(err)
	}
	return jiraTabModel(t).WithTimesheet(sh), srv
}

// feed feeds cmd's message (a batch's, in turn) back into m.
func feed(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = feed(t, m, c)
		}
		return m
	case timerTickMsg:
		return m // the next tick, not now
	default:
		out, next := m.Update(msg)
		return feed(t, out.(Model), next)
	}
}

// TestServerTimer: T starts the timesheet's timer, its stop logs the time on
// that running entry, and a switch made elsewhere shows on the next tick.
func TestServerTimer(t *testing.T) {
	m, srv := sheetModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "T"))
	m = feed(t, out.(Model), cmd)
	srv.Lock()
	r := srv.Running()
	srv.Unlock()
	if r == nil || r.Key != "ABC-1" || m.timer.key != "ABC-1" || m.timer.id != r.ID {
		t.Fatalf("after T: server %+v, timer %+v", r, m.timer)
	}
	if v, _, _ := m.store.GetMeta(timerMeta); !strings.HasSuffix(v, " "+r.ID) {
		t.Errorf("state file %q lacks the entry's id", v)
	}

	// Switched to another issue elsewhere (easyreg's own page).
	srv.Lock()
	start := time.Now().Add(-25 * time.Minute).Truncate(time.Second)
	r.Start = start.Add(-time.Hour)
	r.End = &start
	srv.Entries = append(srv.Entries, &sheettest.Entry{ID: "99", Key: "ABC-2", Start: start})
	srv.Unlock()
	m = feed(t, m, m.syncTimer()) // the tick's read
	if m.timer.key != "ABC-2" || m.timer.id != "99" {
		t.Fatalf("after a switch elsewhere: %+v", m.timer)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "⏱ ABC-2 25m") {
		t.Errorf("header lacks the switched timer:\n%s", v)
	}

	// The stop logs on the running entry, not a new one. (T on the
	// board's ABC-1 would switch the timer there after.)
	m.toggleTimer("")
	m.jiraFieldInput.SetValue("30m review")
	out, cmd = m.applyJiraField()
	m = feed(t, out.(Model), cmd)
	srv.Lock()
	defer srv.Unlock()
	e := srv.Entries[len(srv.Entries)-1]
	if len(srv.Entries) != 2 || srv.Running() != nil || e.End == nil || e.End.Sub(e.Start) != 30*time.Minute || e.Description != "review" {
		t.Fatalf("after the stop: %+v", e)
	}
	if m.timer.key != "" {
		t.Errorf("timer after the stop = %+v", m.timer)
	}
}

// TestServerTimerDrop: ctrl+d at the stop prompt deletes the running entry.
func TestServerTimerDrop(t *testing.T) {
	m, srv := sheetModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "T"))
	m = feed(t, out.(Model), cmd)
	out, _ = m.handleJiraKey(keyMsg(t, "T"))
	m = out.(Model)
	out, cmd, ok := m.timerStopKey(keyMsg(t, "ctrl+d"))
	if !ok {
		t.Fatal("ctrl+d not taken")
	}
	m = feed(t, out.(Model), cmd)
	srv.Lock()
	defer srv.Unlock()
	if len(srv.Entries) != 0 || m.timer.key != "" {
		t.Fatalf("after the drop: server %+v, timer %+v", srv.Entries, m.timer)
	}
}
