package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// TestTeamWalk: the Team standup walks the columns right to left; a card in
// progress shows with its age (stale past stale days) or "no activity", a
// done or to-do card only with activity (a status back where it was, a rank
// or parent set are none); by person groups the same cards.
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
		{Key: "ABC-8", Summary: "Aside", Who: "Ann", What: "commented: here", When: now.Add(-time.Hour)},
		{Key: "ABC-7", Summary: "Bulk", Who: "Ann", What: "points", Changes: []jira.Change{{Field: "Story point estimate", To: "3"}}, When: now.Add(-time.Hour)},
		// Noise: a status back where it was, a rank and a parent set.
		{Key: "ABC-1", Summary: "Idle", Who: "Ann", What: "status", Changes: []jira.Change{{Field: "status", From: "To Do", To: "In progress"}, {Field: "status", From: "In progress", To: "To Do"}}, When: now.Add(-time.Hour)},
		{Key: "ABC-1", Summary: "Idle", Who: "Ann", What: "rank", Changes: []jira.Change{{Field: "Rank", From: "", To: "x"}, {Field: "IssueParentAssociation", To: "ABC-9"}}, When: now.Add(-time.Hour)},
	}
	items, folded, text := teamWalk(cols, entries, []string{"ABC"}, false, 5, since, now)
	want := `Done
- ABC-6 Fresh · Ann · In progress → Done

In progress
- ABC-3 Stuck · Ann · 9d stale · flagged · no activity
- ABC-4 Moving · Bob · 1d · PR open · logged 2h

To do
- ABC-2 Picked · Bob · commented`
	if text != want {
		t.Errorf("walk:\n%s\nwant\n%s", text, want)
	}
	// Off the board: folded, the board's projects only, bulk edits left out.
	if last := items[len(items)-1]; !last.unfold || last.head != "Off the board (1)" ||
		len(folded) != 1 || folded[0].text() != "ABC-8 Aside · Ann · commented" {
		t.Errorf("off the board: %+v, folded %+v", last, folded)
	}
	items, _, text = teamWalk(cols, entries, []string{"ABC"}, true, 5, since, now)
	if !strings.HasPrefix(text, "Ann\n- ABC-6 Fresh · Done · In progress → Done\n- ABC-3 Stuck · In progress · 9d stale") ||
		!strings.Contains(text, "Bob · logged 2h\n- ABC-4 Moving · In progress") {
		t.Errorf("by person:\n%s", text)
	}
	if items[0].head != "Ann (2)" || items[1].key != "ABC-6" {
		t.Errorf("items = %+v", items[:2])
	}
}
