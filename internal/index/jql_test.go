package index

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// TestJQL: the clauses the index knows narrow it; the others are dropped
// and named, never narrowing.
func TestJQL(t *testing.T) {
	ix, _ := openTemp(t)
	now := time.Now()
	ix.PutCards([]jira.Card{
		{Key: "ABC-1", Summary: "Login page broken", Status: "To Do", Type: "Bug", Assignee: "Ada", AssigneeID: "a1", Updated: now},
		{Key: "ABC-2", Summary: "Checkout flow", Status: "In Progress", Type: "Story", InProgress: true, Assignee: "Bob", AssigneeID: "b1", Updated: now.Add(-time.Hour)},
		{Key: "ABC-3", Summary: "Login audit", Status: "Done", Type: "Task", Done: true, Updated: now.Add(-2 * time.Hour)},
		{Key: "XY-4", Summary: "Login elsewhere", Status: "To Do", Type: "Bug", Assignee: "Ada", AssigneeID: "a1", Updated: now.Add(-3 * time.Hour)},
	})
	for _, tc := range []struct {
		q, want, dropped string
	}{
		{`project = ABC ORDER BY rank`, "ABC-1 ABC-2 ABC-3", ""},
		{`project in (ABC, XY) AND assignee = currentUser()`, "ABC-1 XY-4", ""},
		{`project = abc AND statusCategory != Done`, "ABC-1 ABC-2", ""},
		{`statusCategory = "In Progress"`, "ABC-2", ""},
		{`status in ("To Do", Done) AND type = Bug`, "ABC-1 XY-4", ""},
		{`text ~ "login" AND assignee is EMPTY`, "ABC-3", ""},
		{`assignee = Bob`, "ABC-2", ""},
		{`project = ABC AND labels = ui AND (status = Done OR updated >= -7d) order by updated DESC`, "ABC-1 ABC-2 ABC-3",
			"labels = ui|status = Done OR updated >= -7d"},
		{`summary ~ "and" AND project = XY`, "", ""},
		{`project = XY AND text ~ "login" OR x`, "XY-4", `text ~ "login" OR x`},
	} {
		hits, dropped, err := ix.JQL(tc.q, "a1", 50)
		if err != nil {
			t.Fatalf("%s: %v", tc.q, err)
		}
		var keys []string
		for _, h := range hits {
			keys = append(keys, h.Card.Key)
		}
		if got := strings.Join(keys, " "); got != tc.want || strings.Join(dropped, "|") != tc.dropped {
			t.Errorf("%s = %q, dropped %q", tc.q, got, dropped)
		}
	}
	if _, dropped, _ := ix.JQL(`assignee = currentUser()`, "", 50); !slices.Equal(dropped, []string{"assignee = currentUser()"}) {
		t.Errorf("without me: dropped %q", dropped)
	}
}
