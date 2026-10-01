package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/store"
)

// TestTimer: the timer goes in the state file as the TUI keeps it.
func TestTimer(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(context.Background(), Options{Store: st, Site: "demo", Demo: true}))
	t.Cleanup(ts.Close)
	var got workTimer
	if issueCall(t, "GET", ts.URL+"/api/timer", nil, &got); got.Key != "" {
		t.Fatalf("no timer = %+v", got)
	}
	start := time.Unix(1700000000, 0)
	if code := issueCall(t, "PUT", ts.URL+"/api/timer", workTimer{"DEMO-3", start}, nil); code != 200 {
		t.Fatalf("put: %d", code)
	}
	if v, _, _ := st.GetMeta(timerMeta); v != "DEMO-3 1700000000" {
		t.Errorf("stored %q, want the TUI's form", v)
	}
	if issueCall(t, "GET", ts.URL+"/api/timer", nil, &got); got.Key != "DEMO-3" || !got.Start.Equal(start) {
		t.Fatalf("timer = %+v", got)
	}
	issueCall(t, "PUT", ts.URL+"/api/timer", workTimer{}, nil)
	got = workTimer{}
	if issueCall(t, "GET", ts.URL+"/api/timer", nil, &got); got.Key != "" {
		t.Errorf("stopped timer = %+v", got)
	}
	if code := issueCall(t, "PUT", ts.URL+"/api/timer", workTimer{Key: "nope", Start: start}, nil); code != 400 {
		t.Errorf("bad key: %d", code)
	}
}
