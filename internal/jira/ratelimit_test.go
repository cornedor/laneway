package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetry makes the retry waits test-sized.
func fastRetry(t *testing.T) {
	w, m := retryWait, maxRetryWait
	retryWait, maxRetryWait = time.Millisecond, 2*time.Second
	t.Cleanup(func() { retryWait, maxRetryWait = w, m })
}

// statusThen answers code (with Retry-After retry, when set) to the first
// n requests, then 200 {}; hits counts them all.
func statusThen(t *testing.T, code, n int, retry string, hits *atomic.Int32) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(hits.Add(1)) <= n {
			if retry != "" {
				w.Header().Set("Retry-After", retry)
			}
			w.WriteHeader(code)
			return
		}
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
}

// TestRetryOn429: a 429 is sent again after its Retry-After, up to twice,
// a write too; one past maxRetryWait fails at once with the wait in it.
func TestRetryOn429(t *testing.T) {
	fastRetry(t)
	for _, c := range []struct {
		name, method, retry string
		refused, hits       int32
		ok                  bool
	}{
		{"read", http.MethodGet, "0", 1, 2, true},
		{"no Retry-After", http.MethodGet, "", 2, 3, true},
		{"write", http.MethodPut, "0", 1, 2, true},
		{"three times", http.MethodGet, "0", 3, 3, false},
		{"too long", http.MethodGet, "60", 1, 1, false},
	} {
		var hits atomic.Int32
		cl := statusThen(t, http.StatusTooManyRequests, int(c.refused), c.retry, &hits)
		_, err := cl.send(context.Background(), c.method, "/x", "ABC-1", nil, false)
		if (err == nil) != c.ok || hits.Load() != c.hits {
			t.Errorf("%s: %d requests, %v; want %d, ok %v", c.name, hits.Load(), err, c.hits, c.ok)
		}
		if c.name == "too long" && (err == nil || !strings.Contains(err.Error(), "retry in 60s")) {
			t.Errorf("too long: %v, want the wait said", err)
		}
	}
}

// TestRetryOn503: a 503 is sent again when it reads, never when it
// writes; a 500 never.
func TestRetryOn503(t *testing.T) {
	fastRetry(t)
	for _, c := range []struct {
		method string
		code   int
		hits   int32
	}{
		{http.MethodGet, http.StatusServiceUnavailable, 2},
		{http.MethodPost, http.StatusServiceUnavailable, 1},
		{http.MethodGet, http.StatusInternalServerError, 1},
	} {
		var hits atomic.Int32
		cl := statusThen(t, c.code, 1, "", &hits)
		cl.send(context.Background(), c.method, "/x", "ABC-1", map[string]string{"a": "b"}, false)
		if hits.Load() != c.hits {
			t.Errorf("%s %d: %d requests, want %d", c.method, c.code, hits.Load(), c.hits)
		}
	}
}

// TestRetryResendsBody: the retried write carries its body again.
func TestRetryResendsBody(t *testing.T) {
	fastRetry(t)
	var bodies []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if _, err := c.send(context.Background(), http.MethodPut, "/x", "ABC-1", map[string]string{"a": "b"}, false); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[1] != `{"a":"b"}` {
		t.Errorf("bodies %q", bodies)
	}
}

// TestRetryStopsOnCancel: a cancelled caller doesn't sit out the wait.
func TestRetryStopsOnCancel(t *testing.T) {
	var hits atomic.Int32
	c := statusThen(t, http.StatusTooManyRequests, 9, "5", &hits)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.send(ctx, http.MethodGet, "/x", "ABC-1", nil, false); err == nil {
		t.Fatal("want the 429")
	}
	if d := time.Since(start); d > time.Second || hits.Load() != 1 {
		t.Errorf("%d requests in %v", hits.Load(), d)
	}
}

// TestInFlightCap: however many ask at once, at most inFlight requests are
// on their way.
func TestInFlightCap(t *testing.T) {
	var now, most atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := now.Add(1)
		defer now.Add(-1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			if _, err := c.send(context.Background(), http.MethodGet, "/x", "x", nil, false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if m := most.Load(); m > inFlight || m < 2 {
		t.Errorf("%d at once, want 2..%d", m, inFlight)
	}
}

// TestSharedLookups: who you are and the field metadata are asked once,
// however many ask at once and whichever feature asks.
func TestSharedLookups(t *testing.T) {
	var myself, field atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		switch r.URL.Path {
		case "/rest/api/3/myself":
			myself.Add(1)
			io.WriteString(w, `{"accountId":"me","displayName":"Me"}`)
		case "/rest/api/3/field":
			field.Add(1)
			io.WriteString(w, `[{"id":"customfield_1","name":"Story Points"},{"id":"customfield_2","name":"Start date"}]`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if u, err := c.Myself(ctx); err != nil || u.AccountID != "me" {
				t.Errorf("Myself = %+v, %v", u, err)
			}
		})
		wg.Go(func() { c.StoryPointsField(ctx) })
		wg.Go(func() { c.resolveRoadmapFields(ctx) })
	}
	wg.Wait()
	if f := c.StoryPointsField(ctx); f != "customfield_1" {
		t.Errorf("story points = %q", f)
	}
	if ids, _ := c.resolveRoadmapFields(ctx); ids.start != "customfield_2" {
		t.Errorf("start = %q", ids.start)
	}
	if myself.Load() != 1 || field.Load() != 1 {
		t.Errorf("/myself %d times, /field %d times; want once each", myself.Load(), field.Load())
	}
}
