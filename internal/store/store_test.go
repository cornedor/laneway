package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConcurrentWriters: two stores on one file never leave it half written,
// and no tmp file stays behind.
func TestConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, s := range []*Store{a, b} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			for i := range 200 {
				if err := s.SetMeta("k", fmt.Sprintf("%p-%d", s, i)); err != nil {
					t.Error(err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.GetMeta("k"); !ok {
		t.Fatal("state lost: file did not parse")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("left behind %d files, want only state.json", len(entries))
	}
}

// TestSecondProcess: two stores on one file (the TUI beside laneway web)
// keep each other's keys and read each other's writes.
func TestSecondProcess(t *testing.T) {
	recheck = 0
	t.Cleanup(func() { recheck = time.Second })
	path := filepath.Join(t.TempDir(), "state.json")
	a, _ := Open(path)
	b, _ := Open(path)
	if err := a.SetMeta("timer", "ABC-1 100"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetMeta("draft", "hello"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := b.GetMeta("timer"); v != "ABC-1 100" {
		t.Fatalf("b reads timer %q, want a's write", v)
	}
	if v, _, _ := a.GetMeta("draft"); v != "hello" {
		t.Fatalf("a reads draft %q, want b's write", v)
	}
	if err := a.DeleteMeta("timer"); err != nil {
		t.Fatal(err)
	}
	c, _ := Open(path)
	if _, ok, _ := c.GetMeta("timer"); ok {
		t.Fatal("timer still in the file after a deleted it")
	}
	if v, _, _ := c.GetMeta("draft"); v != "hello" {
		t.Fatalf("draft %q after a's delete, want b's write kept", v)
	}
}
