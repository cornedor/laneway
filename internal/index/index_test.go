package index

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

func openTemp(t *testing.T) (*Index, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index-test.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix, path
}

func TestPutCardsReplaces(t *testing.T) {
	ix, _ := openTemp(t)
	ix.PutCards([]jira.Card{{Key: "ABC-1", Summary: "old", ParentKey: "ABC-9"}, {Key: "XY-2", Summary: "other"}})
	ix.PutCards([]jira.Card{{Key: "ABC-1", Summary: "new"}})
	c, ok := ix.Get("ABC-1")
	if !ok || c.Summary != "new" || c.ParentKey != "" {
		t.Fatalf("Get = %+v, %v; want the second read whole", c, ok)
	}
	if ix.Synced("ABC-1").IsZero() || !ix.Synced("NOPE-1").IsZero() {
		t.Error("Synced: want a time for an indexed key, zero for another")
	}
	n, err := ix.Stats()
	if err != nil || n["ABC"] != 1 || n["XY"] != 1 {
		t.Errorf("Stats = %v, %v", n, err)
	}
}

func TestPutIssueKeepsCardFields(t *testing.T) {
	ix, _ := openTemp(t)
	ix.PutCards([]jira.Card{{Key: "ABC-1", Summary: "old", ParentKey: "ABC-9", Sprint: "S1"}})
	up := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	ix.PutIssue(&jira.Issue{Key: "ABC-1", Summary: "renamed", Status: "Done", StatusCategory: "done", Labels: []string{"a", "b"}, Updated: up})
	c, _ := ix.Get("ABC-1")
	if c.Summary != "renamed" || !c.Done || c.Labels != "a b" || !c.Updated.Equal(up) || c.ParentKey != "ABC-9" || c.Sprint != "S1" {
		t.Errorf("Get = %+v; want the issue's fields over the card's", c)
	}
	ix.PutIssue(&jira.Issue{Key: "NEW-1", Summary: "fresh"})
	if c, ok := ix.Get("NEW-1"); !ok || c.Summary != "fresh" {
		t.Errorf("an unindexed issue: Get = %+v, %v", c, ok)
	}
}

func TestReopenAndClear(t *testing.T) {
	ix, path := openTemp(t)
	ix.PutCards([]jira.Card{{Key: "ABC-1", Summary: "kept"}})
	ix.Close()
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := ix.Get("ABC-1"); !ok || c.Summary != "kept" {
		t.Fatalf("after reopening: Get = %+v, %v", c, ok)
	}
	ix.Close()
	if err := Clear(path); err != nil {
		t.Fatal(err)
	}
	if err := Clear(path); err != nil {
		t.Errorf("clearing a cleared index: %v", err)
	}
	ix, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, ok := ix.Get("ABC-1"); ok {
		t.Error("after Clear the issue is still indexed")
	}
}

func TestNilIndex(t *testing.T) {
	var ix *Index
	ix.PutCards([]jira.Card{{Key: "ABC-1"}})
	ix.PutIssue(&jira.Issue{Key: "ABC-1"})
	if _, ok := ix.Get("ABC-1"); ok || ix.Close() != nil {
		t.Error("a nil index should hold nothing")
	}
}

func TestSearch(t *testing.T) {
	ix, _ := openTemp(t)
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	ix.PutCards([]jira.Card{
		{Key: "ABC-1", Summary: "Login page broken", Updated: day(1)},
		{Key: "XY-7", Summary: "login with SSO", Updated: day(3)},
		{Key: "ABC-2", Summary: "100% width_bar", Updated: day(2)},
	})
	keys := func(hs []Hit) (out []string) {
		for _, h := range hs {
			out = append(out, h.Card.Key)
		}
		return out
	}
	for _, tc := range []struct {
		q    string
		want string
	}{
		{"LOGIN", "XY-7 ABC-1"},
		{"login broken", "ABC-1"},
		{"xy-7", "XY-7"},
		{"%", "ABC-2"},
		{"h_b", "ABC-2"},
		{"nothing", ""},
		{"  ", ""},
	} {
		hs, err := ix.Search(tc.q, 10)
		if got := strings.Join(keys(hs), " "); err != nil || got != tc.want {
			t.Errorf("Search(%q) = %q, %v; want %q", tc.q, got, err, tc.want)
		}
	}
	if hs, _ := ix.Search("login", 1); len(hs) != 1 || hs[0].Synced.IsZero() {
		t.Errorf("Search limit 1 = %+v", hs)
	}
}

// TestUsers: a sync sets who is assignable; people seen on issues are kept
// by name, renamed as seen, without becoming assignable; a query matches
// word starts, names starting with it first.
func TestUsers(t *testing.T) {
	ix, _ := openTemp(t)
	if !ix.UsersSynced("ABC").IsZero() {
		t.Fatal("synced before any sync")
	}
	ix.SyncAssignable("ABC", []jira.User{{AccountID: "a1", DisplayName: "Ada Lovelace"}, {AccountID: "c1", DisplayName: "Claude"}, {AccountID: "g1", DisplayName: "Grace Hopper"}})
	ix.PutIssue(&jira.Issue{Key: "ABC-1", ReporterAccountID: "r1", Reporter: "Rita Clark",
		Comments: []jira.Comment{{AuthorID: "g1", Author: "Grace B. Hopper"}}})
	if ix.UsersSynced("ABC").IsZero() {
		t.Error("sync not noted")
	}
	names := func(us []jira.User) string {
		var out []string
		for _, u := range us {
			out = append(out, u.DisplayName)
		}
		return strings.Join(out, ", ")
	}
	if got := names(ix.Users("ABC", "", true)); got != "Ada Lovelace, Claude, Grace B. Hopper" {
		t.Errorf("assignable = %s", got)
	}
	if got := names(ix.Users("ABC", "cl", false)); got != "Claude, Rita Clark" {
		t.Errorf("cl = %s", got)
	}
	if got := names(ix.Users("ABC", "hop gr", true)); got != "Grace B. Hopper" {
		t.Errorf("hop gr = %s", got)
	}
	if got := names(ix.Users("XY", "", false)); got != "" {
		t.Errorf("other project = %s", got)
	}
	ix.SyncAssignable("ABC", []jira.User{{AccountID: "c1", DisplayName: "Claude"}})
	if got := names(ix.Users("ABC", "", true)); got != "Claude" {
		t.Errorf("after a resync = %s; want the ones gone no longer assignable", got)
	}
	if n, _ := ix.PeopleStats(); n["ABC"] != 4 {
		t.Errorf("PeopleStats = %v", n)
	}
}
