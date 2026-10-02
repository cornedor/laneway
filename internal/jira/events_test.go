package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/laneway/1/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": connected\n\n")
		fmt.Fprint(w, "id: 7\nevent: issue\ndata: {\"seq\":7,\"kind\":\"issue\",\"action\":\"put\",\"id\":\"10001\",\"key\":\"ABC-1\",\"projectId\":10000}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "a@b.c", APIToken: "t"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := make(chan SiteChange, 1)
	go c.Events(ctx, func(ch SiteChange) { got <- ch; cancel() })
	select {
	case ch := <-got:
		if ch.Key != "ABC-1" || ch.Seq != 7 || ch.Kind != "issue" {
			t.Errorf("change %+v", ch)
		}
	case <-ctx.Done():
		t.Fatal("no change read")
	}

	jira := httptest.NewServer(http.NotFoundHandler())
	defer jira.Close()
	c = New(Config{BaseURL: jira.URL, Email: "a@b.c", APIToken: "t"})
	if err := c.Events(context.Background(), func(SiteChange) {}); err != ErrNoEvents {
		t.Errorf("against Jira: %v, want ErrNoEvents", err)
	}
}
