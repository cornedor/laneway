package index

import (
	"path/filepath"
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
