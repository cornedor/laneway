package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// TestTeamWalk: the Team standup walks the columns right to left; a card in
// progress shows with its age (stale past stale days) or "no activity", a
// done or to-do card only with activity; by person groups the same cards.
func TestTeamWalk(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.Local)
	since := now.AddDate(0, 0, -3)
	cols := []teamColumn{
		{"To do", []jira.Card{{Key: "ABC-1", Summary: "Idle"}, {Key: "ABC-2", Summary: "Picked", Assignee: "Bob"}}},
		{"In progress", []jira.Card{
			{Key: "ABC-3", Summary: "Stuck", Assignee: "Ann", InProgress: true, Since: now.AddDate(0, 0, -9), Flagged: true},
			{Key: "ABC-4", Summary: "Moving", Assignee: "Bob", InProgress: true, Since: now.AddDate(0, 0, -1), PR: "open"}}},
		{"Done", []jira.Card{{Key: "ABC-5", Summary: "Old", Done: true}, {Key: "ABC-6", Summary: "Fresh", Assignee: "Ann", Done: true}}},
	}
	entries := []jira.InboxEntry{
		{Key: "ABC-2", Summary: "Picked", Who: "Bob", What: "commented: hi", When: now.Add(-time.Hour)},
		{Key: "ABC-4", Summary: "Moving", Who: "Bob", Logged: 7200, What: "logged 2h", When: now.Add(-2 * time.Hour)},
		{Key: "ABC-6", Summary: "Fresh", Who: "Ann", What: "status", Changes: []jira.Change{{Field: "status", From: "In progress", To: "Done"}}, When: now.Add(-3 * time.Hour)},
		{Key: "XYZ-9", Summary: "Elsewhere", Who: "Ann", What: "commented: there", When: now.Add(-time.Hour)},
	}
	_, text := teamWalk(cols, entries, false, 5, since, now)
	want := `Done
- ABC-6 Fresh · Ann · In progress → Done

In progress
- ABC-3 Stuck · Ann · 9d stale · flagged · no activity
- ABC-4 Moving · Bob · 1d · PR open · logged 2h

To do
- ABC-2 Picked · Bob · commented

Off the board
- XYZ-9 Elsewhere · Ann · commented`
	if text != want {
		t.Errorf("walk:\n%s\nwant\n%s", text, want)
	}
	items, text := teamWalk(cols, entries, true, 5, since, now)
	if !strings.HasPrefix(text, "Ann\n- ABC-6 Fresh · Done · In progress → Done\n- ABC-3 Stuck · In progress · 9d stale") ||
		!strings.Contains(text, "Bob · logged 2h\n- ABC-4 Moving · In progress") {
		t.Errorf("by person:\n%s", text)
	}
	if items[0].label != "── Ann (2)" || items[1].id != "ABC-6" {
		t.Errorf("items = %+v", items[:2])
	}
}
