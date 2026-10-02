package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

func init() {
	handle("GET /api/site/events", siteEvents)
}

// siteEvents passes the site's change stream (laneway-server; Jira has
// none) on to the browser: "change" events, or one "none" when the site
// has no stream, after which the page keeps polling.
func siteEvents(s *Server, w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	c := s.ClientFor(r)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	changes := make(chan jira.SiteChange, 64)
	done := make(chan error, 1)
	go func() {
		done <- c.Events(ctx, func(ch jira.SiteChange) {
			select {
			case changes <- ch:
			default: // the page refreshes on the next one anyway
			}
		})
	}()
	fmt.Fprint(w, "retry: 30000\n\n")
	fl.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-done:
			if errors.Is(err, jira.ErrNoEvents) {
				fmt.Fprint(w, "event: none\ndata: {}\n\n")
				fl.Flush()
			}
			return
		case ch := <-changes:
			b, _ := json.Marshal(ch)
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", b)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
