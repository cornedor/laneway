package index

import (
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

func TestProjectSyncRoundTrip(t *testing.T) {
	ix, path := openTemp(t)
	if s := ix.ProjectSync("ABC"); s.Complete || !s.At.IsZero() {
		t.Fatalf("never synced: %+v, want zero", s)
	}
	at := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	if err := ix.SetProjectSync("ABC", ProjectSync{At: at, KeysAt: at.Add(-time.Hour), Complete: true}); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	s := ix.ProjectSync("ABC")
	if !s.Complete || !s.At.Equal(at) || !s.KeysAt.Equal(at.Add(-time.Hour)) {
		t.Errorf("after reopening: %+v", s)
	}
	if ix.ProjectSync("XY").Complete {
		t.Error("another project: want it unsynced")
	}
}

func TestProjectCardsNewestFirst(t *testing.T) {
	ix, _ := openTemp(t)
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	ix.PutCards([]jira.Card{
		{Key: "ABC-1", Updated: day(1), Resolution: "Done", Resolved: day(2)},
		{Key: "ABC-2", Updated: day(9)},
		{Key: "XY-1", Updated: day(5)},
	})
	cards, err := ix.ProjectCards("ABC")
	if err != nil || len(cards) != 2 || cards[0].Key != "ABC-2" || cards[1].Key != "ABC-1" {
		t.Fatalf("ProjectCards = %+v, %v; want ABC-2, ABC-1", cards, err)
	}
	if cards[1].Resolution != "Done" || !cards[1].Resolved.Equal(day(2)) {
		t.Errorf("resolution lost: %+v", cards[1])
	}
}

func TestDropUnreadAndKeepKeys(t *testing.T) {
	ix, _ := openTemp(t)
	ix.PutCards([]jira.Card{{Key: "ABC-1"}, {Key: "ABC-2"}, {Key: "XY-1"}})
	since := time.Now().Add(time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	ix.PutCards([]jira.Card{{Key: "ABC-2"}})
	if n, err := ix.DropUnread("ABC", since); err != nil || n != 1 {
		t.Fatalf("DropUnread = %d, %v; want ABC-1 dropped", n, err)
	}
	if _, ok := ix.Get("ABC-1"); ok {
		t.Error("ABC-1 still there")
	}
	if _, ok := ix.Get("XY-1"); !ok {
		t.Error("another project's issue was dropped")
	}
	ix.PutCards([]jira.Card{{Key: "ABC-3"}})
	if n, err := ix.KeepKeys("ABC", []string{"ABC-3"}); err != nil || n != 1 {
		t.Fatalf("KeepKeys = %d, %v; want ABC-2 dropped", n, err)
	}
	if cards, _ := ix.ProjectCards("ABC"); len(cards) != 1 || cards[0].Key != "ABC-3" {
		t.Errorf("left: %+v", cards)
	}
}
