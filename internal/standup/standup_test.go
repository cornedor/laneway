package standup

import (
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

var now = time.Date(2026, 9, 28, 10, 0, 0, 0, time.Local) // a Monday

func board() Board {
	return Board{Projects: []string{"ABC"}, Stale: 5, Columns: []Column{
		{"To do", []jira.Card{{Key: "ABC-1", Summary: "Idle"}, {Key: "ABC-2", Summary: "Picked", Assignee: "Bob", AssigneeID: "b"}}},
		{"In progress", []jira.Card{
			{Key: "ABC-3", Summary: "Stuck", Assignee: "Ann", AssigneeID: "a", InProgress: true, Since: now.AddDate(0, 0, -9), Flagged: true},
			{Key: "ABC-4", Summary: "Moving", Assignee: "Bob", AssigneeID: "b", InProgress: true, Since: now.AddDate(0, 0, -1), PR: "open"}}},
		{"Done", []jira.Card{{Key: "ABC-5", Summary: "Old", Done: true}, {Key: "ABC-6", Summary: "Fresh", Assignee: "Ann", AssigneeID: "a", Done: true}}},
	}}
}

var entries = []jira.InboxEntry{
	{Key: "ABC-2", Summary: "Picked", Who: "Bob", WhoID: "b", What: "commented: hi", When: now.Add(-time.Hour)},
	{Key: "ABC-4", Summary: "Moving", Who: "Bob", WhoID: "b", Logged: 7200, What: "logged 2h", When: now.Add(-2 * time.Hour)},
	{Key: "ABC-4", Summary: "Moving", Who: "Ann", WhoID: "a", What: "commented: lgtm", When: now.Add(-time.Hour)},
	{Key: "ABC-6", Summary: "Fresh", Who: "Ann", WhoID: "a", What: "status", Changes: []jira.Change{{Field: "status", From: "In progress", To: "Done"}}, When: now.Add(-3 * time.Hour)},
	{Key: "XYZ-9", Summary: "Elsewhere", Who: "Ann", WhoID: "a", What: "commented: there", When: now.Add(-time.Hour)},
	{Key: "ABC-8", Summary: "Aside", Who: "Ann", WhoID: "a", What: "commented: here", When: now.Add(-time.Hour)},
	{Key: "ABC-7", Summary: "Bulk", Who: "Ann", WhoID: "a", What: "points", Changes: []jira.Change{{Field: "Story point estimate", To: "3"}}, When: now.Add(-time.Hour)},
	// Noise: a status back where it was, a rank and a parent set.
	{Key: "ABC-1", Summary: "Idle", Who: "Ann", WhoID: "a", What: "status", Changes: []jira.Change{{Field: "status", From: "To Do", To: "In progress"}, {Field: "status", From: "In progress", To: "To Do"}}, When: now.Add(-time.Hour)},
	{Key: "ABC-1", Summary: "Idle", Who: "Ann", WhoID: "a", What: "rank", Changes: []jira.Change{{Field: "Rank", From: "", To: "x"}, {Field: "IssueParentAssociation", To: "ABC-9"}}, When: now.Add(-time.Hour)},
}

// TestEveryone walks the columns right to left; a card in progress shows
// with its age (stale past stale days) or "no activity", a done or to-do
// card only with activity (a status back where it was, a rank or parent set
// are none); Off the board folds the projects' other issues, bulk edits
// left out.
func TestEveryone(t *testing.T) {
	st := Stops(board(), nil, entries, now.AddDate(0, 0, -3), now)[0]
	want := `Done
- ABC-6 Fresh · Ann · In progress → Done

In progress
- ABC-3 Stuck · Ann · 9d stale · flagged · no activity
- ABC-4 Moving · Bob · 1d · PR open · logged 2h, commented

To do
- ABC-2 Picked · Bob · commented`
	if st.Text != want {
		t.Errorf("walk:\n%s\nwant\n%s", st.Text, want)
	}
	if last := st.Rows[len(st.Rows)-1]; !last.Unfold || last.Head != "Off the board (1)" ||
		len(st.Folded) != 1 || st.Folded[0].Text() != "ABC-8 Aside · Ann · commented" {
		t.Errorf("off the board: %+v, folded %+v", last, st.Folded)
	}
	if st.Person.ID != "" || st.Quiet {
		t.Errorf("everyone's stop: %+v", st)
	}
}

// TestPersonStop: a person's stop is their cards and those they did
// something on; someone without activity is quiet, their cards still
// listed.
func TestPersonStop(t *testing.T) {
	b := board()
	people := People(b.Columns)
	if len(people) != 2 || people[0].Name != "Ann" || people[1].Name != "Bob" {
		t.Fatalf("people in walk order: %+v", people)
	}
	stops := Stops(b, people, entries, now.AddDate(0, 0, -3), now)
	ann := stops[1]
	if want := "Done\n- ABC-6 Fresh · Ann · In progress → Done\n\nIn progress\n- ABC-3 Stuck · Ann · 9d stale · flagged · no activity\n- ABC-4 Moving · Bob · 1d · PR open · logged 2h, commented"; ann.Text != want {
		t.Errorf("Ann:\n%s\nwant\n%s", ann.Text, want)
	}
	if len(ann.Folded) != 1 || ann.Quiet {
		t.Errorf("Ann's off the board %+v, quiet %v", ann.Folded, ann.Quiet)
	}
	bob := stops[2]
	if strings.Contains(bob.Text, "ABC-3") || !strings.Contains(bob.Text, "ABC-2 Picked") || len(bob.Folded) != 0 {
		t.Errorf("Bob:\n%s\n%+v", bob.Text, bob.Folded)
	}
	quiet := Stops(b, people, entries[:1], now.AddDate(0, 0, -3), now)[1]
	if !quiet.Quiet || !strings.Contains(quiet.Text, "ABC-3 Stuck") {
		t.Errorf("Ann without activity: %+v", quiet)
	}
}

// TestBlockers: a blocked card joins the walk, even not started, and says
// what blocks it.
func TestBlockers(t *testing.T) {
	b := Board{Projects: []string{"ABC"}, Columns: []Column{{"To do", []jira.Card{{Key: "ABC-1", Summary: "Waiting", Assignee: "Bob"}, {Key: "ABC-2", Summary: "Idle"}}}},
		Blockers: map[string][]string{"ABC-1": {"XY-3", "XY-4"}}}
	if st := Stops(b, nil, nil, now.AddDate(0, 0, -1), now)[0]; st.Text != "To do\n- ABC-1 Waiting · Bob · blocked by XY-3, XY-4 · no activity" || !st.Quiet {
		t.Errorf("walk:\n%s", st.Text)
	}
}

// TestMine: your commits count as yours on your stop.
func TestMine(t *testing.T) {
	got := Mine(nil, []jira.InboxEntry{{Key: "ABC-2", What: "commit: fix", When: now}}, "b", "Bob")
	st := Stops(board(), []Person{{ID: "b", Name: "Bob"}}, got, now.AddDate(0, 0, -1), now)[1]
	if !strings.Contains(st.Text, "ABC-2 Picked · Bob · 1 commit") || st.Quiet {
		t.Errorf("with commits:\n%s", st.Text)
	}
}

func TestSince(t *testing.T) {
	if got := Since(now, nil, 1); got.Weekday() != time.Friday {
		t.Errorf("Monday's lookback: %v", got)
	}
	if got := Since(now, nil, 2); got.Weekday() != time.Thursday {
		t.Errorf("two workdays back: %v", got)
	}
}

func TestSettings(t *testing.T) {
	s, warn := Parse(config.UIConfig{})
	if s != Defaults || warn != nil || s.Turn(5) != 3*time.Minute {
		t.Errorf("defaults %+v %v, turn %v", s, warn, s.Turn(5))
	}
	s, warn = Parse(config.UIConfig{StandupStart: "first", StandupLookback: 2, StandupLength: "10m", StandupTimebox: "90s", StandupShuffle: "on"})
	if !s.First || s.Lookback != 2 || s.Length != 10*time.Minute || s.Turn(5) != 90*time.Second || !s.Shuffle || warn != nil {
		t.Errorf("set %+v %v", s, warn)
	}
	s, warn = Parse(config.UIConfig{StandupStart: "me", StandupLookback: 11, StandupLength: "1s", StandupShuffle: "yes"})
	if s != Defaults || len(warn) != 4 {
		t.Errorf("bad values kept %+v, warned %v", s, warn)
	}
}
