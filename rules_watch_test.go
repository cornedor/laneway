package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// TestRulesWatchLoop: the first search is a baseline; a later one that moves
// an issue logs it and runs exec, and highlight is skipped.
func TestRulesWatchLoop(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			fmt.Fprint(w, `[]`)
			return
		}
		status := "To do"
		if calls.Add(1) > 1 {
			status = "Done"
		}
		fmt.Fprintf(w, `{"issues": [{"key": "ABC-1", "fields": {"summary": "One", "status": {"id": %q, "name": %q}}}]}`, status, status)
	}))
	defer srv.Close()
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	set, warn := rules.Compile([]rules.Rule{{Name: "moved", Watch: "x", On: rules.StrList{"status"},
		Actions: []rules.Action{{Type: "log"}, {Type: "highlight"}, {Type: "exec", Command: []string{"touch", marker}}}}})
	if len(warn) > 0 {
		t.Fatal(warn)
	}
	var out syncBuf
	w := &watcher{c: jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"}), set: set,
		log: filepath.Join(dir, "rules.log"), out: &out}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.loop(ctx, rules.Watch{JQL: "x", Every: 5 * time.Millisecond})
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(out.String(), "moved: ABC-1") >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	b, _ := os.ReadFile(w.log)
	if !strings.HasSuffix(string(b), " moved: ABC-1 status To do → Done\n") {
		t.Errorf("log = %q", b)
	}
	if got := out.String(); strings.Count(got, "moved: ABC-1") < 2 {
		t.Errorf("out = %q", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("exec did not run")
	}
}

// syncBuf is a bytes.Buffer safe to read while the watcher writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
