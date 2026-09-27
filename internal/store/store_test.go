package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
