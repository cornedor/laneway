package jira

import (
	"testing"
	"time"
)

var lineCols = []Column{
	{Name: "To Do", StatusIDs: []string{"1"}},
	{Name: "In Progress", StatusIDs: []string{"2"}},
	{Name: "In Review", StatusIDs: []string{"3", "4"}},
	{Name: "Done", StatusIDs: []string{"5"}},
}

func oct(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }

// TestLinePast: a line holds its column's statuses and those right of it;
// live it follows the status, First keeps the first crossing.
func TestLinePast(t *testing.T) {
	// In progress on the 2nd, review on the 3rd, back on the 4th, review
	// (its other status) on the 5th, then skips to done on the 6th.
	is := BurnIssue{Key: "A-1", Status: "5", Moves: []StatusMove{
		{When: oct(2), From: "1", To: "2"}, {When: oct(3), From: "2", To: "3"},
		{When: oct(4), From: "3", To: "2"}, {When: oct(5), From: "2", To: "4"}, {When: oct(6), From: "4", To: "5"},
	}}
	live, first := NewLine(lineCols, "in review", false), NewLine(lineCols, "In Review", true)
	if live == nil || live.Name != "In Review" {
		t.Fatalf("line = %+v", live)
	}
	for _, c := range []struct {
		d           int
		live, first bool
	}{{1, false, false}, {3, true, true}, {4, false, true}, {5, true, true}, {7, true, true}} {
		if got := live.Past(is, oct(c.d)); got != c.live {
			t.Errorf("live past on the %d = %v", c.d, got)
		}
		if got := first.Past(is, oct(c.d)); got != c.first {
			t.Errorf("first past on the %d = %v", c.d, got)
		}
	}
	if at, ok := live.Since(is); !ok || !at.Equal(oct(5)) {
		t.Errorf("live since = %v %v, want the 5th", at, ok)
	}
	if at, ok := first.Since(is); !ok || !at.Equal(oct(3)) {
		t.Errorf("first since = %v %v, want the 3rd", at, ok)
	}
	// Skipping review straight to done still crosses it.
	skip := BurnIssue{Status: "5", Moves: []StatusMove{{When: oct(2), From: "2", To: "5"}}}
	if at, ok := live.Since(skip); !ok || !at.Equal(oct(2)) {
		t.Errorf("skip since = %v %v", at, ok)
	}
	back := BurnIssue{Status: "2", Moves: []StatusMove{{When: oct(2), From: "2", To: "3"}, {When: oct(3), From: "3", To: "2"}}}
	if _, ok := live.Since(back); ok {
		t.Error("moved back: live counts it past")
	}
	if at, ok := first.Since(back); !ok || !at.Equal(oct(2)) {
		t.Errorf("moved back: first since = %v %v", at, ok)
	}
	// A status no column holds stands before every line.
	if live.Past(BurnIssue{Status: "99"}, oct(1)) {
		t.Error("unmapped status is past")
	}
}

// TestLineJira: no line is Jira's done, the resolution, and comes last.
func TestLineJira(t *testing.T) {
	var jira *Line
	if NewLine(lineCols, "", false) != nil || NewLine(lineCols, "Nope", false) != nil {
		t.Fatal("a line for no column")
	}
	is := BurnIssue{Resolved: oct(3)}
	if jira.Past(is, oct(2)) || !jira.Past(is, oct(3)) {
		t.Error("Jira's done is not the resolution")
	}
	if at, ok := jira.Since(is); !ok || !at.Equal(oct(3)) || jira.Label() != "done" {
		t.Errorf("since = %v %v", at, ok)
	}
	review := NewLine(lineCols, "In Review", false)
	if a, b := Order(jira, review); a != review || b != nil {
		t.Error("Jira's done is not last")
	}
	done := NewLine(lineCols, "Done", false)
	if a, b := Order(done, review); a != review || b != done {
		t.Error("lines out of board order")
	}
}
