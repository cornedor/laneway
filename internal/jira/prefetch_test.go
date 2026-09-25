package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPrefetch: keys not freshly cached are fetched once, and Get then
// serves them without a request; a stale entry is fetched again.
func TestPrefetch(t *testing.T) {
	var mu sync.Mutex
	got := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			io.WriteString(w, `[]`)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		mu.Lock()
		got[key]++
		mu.Unlock()
		io.WriteString(w, `{"key":"`+key+`","fields":{"summary":"s"}}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	ctx := context.Background()
	if _, err := c.Get(ctx, "A-1"); err != nil {
		t.Fatal(err)
	}
	c.Prefetch(ctx, []string{"A-1", "A-2", "A-3"})
	for _, k := range []string{"A-2", "A-3"} {
		if _, err := c.Get(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if got["A-1"] != 1 || got["A-2"] != 1 || got["A-3"] != 1 {
		t.Errorf("fetches = %v", got)
	}
	c.mu.Lock()
	e := c.cache["A-1"]
	e.at = time.Now().Add(-issueTTL)
	c.cache["A-1"] = e
	c.mu.Unlock()
	_, _ = c.Get(ctx, "A-1")
	if got["A-1"] != 2 {
		t.Errorf("stale A-1 fetched %d times, want 2", got["A-1"])
	}
}
