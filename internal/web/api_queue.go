package web

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/offline"
)

// Writes that never reached Jira wait in the state file (internal/offline),
// as in the TUI, and are retried every offline.Every while the server runs.

// queueNotes is what the replayer did that the user hasn't necessarily seen:
// writes Jira refused (they are dropped from the queue) and the issue a
// replay stopped at because it changed in Jira. Both also go to the rule
// feed, so the browser's stream toasts them.
type queueNotes struct {
	mu       sync.Mutex
	next     int
	failed   []queueFailure
	conflict string
	feed     *ruleFeed
}

type queueFailure struct {
	ID    int
	What  string
	Error string
	At    time.Time
}

const maxFailures = 20

func (n *queueNotes) record(res offline.Result) {
	n.mu.Lock()
	var evs []RuleEvent
	for _, f := range res.Failed {
		what, msg, _ := strings.Cut(f, ": ")
		n.next++
		n.failed = append(n.failed, queueFailure{n.next, what, msg, time.Now()})
		evs = append(evs, RuleEvent{Rule: "queue", Action: "queue", Title: "Queued write refused", Text: what, Err: msg})
	}
	if len(n.failed) > maxFailures {
		n.failed = n.failed[len(n.failed)-maxFailures:]
	}
	if res.Conflict != "" && res.Conflict != n.conflict {
		evs = append(evs, RuleEvent{Rule: "queue", Action: "queue", Title: "Queued write held", Text: res.Conflict + " changed in Jira since", Key: res.Conflict})
	}
	n.conflict = res.Conflict
	n.mu.Unlock()
	if n.feed != nil {
		for _, e := range evs {
			n.feed.add(e)
		}
	}
}

// runQueue retries the site's queue until ctx ends.
func runQueue(ctx context.Context, o Options, notes *queueNotes) {
	if o.Client == nil || o.Store == nil || o.Demo {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(offline.Every):
		}
		if len(offline.Read(o.Store)) > 0 {
			notes.record(offline.Replay(ctx, o.Client, o.Store, false))
		}
	}
}

type queueItem struct {
	ID     string
	Index  int
	What   string
	Method string
	Path   string
	Key    string
	At     time.Time
}

func queueList(o Options) []queueItem {
	out := []queueItem{}
	for i, w := range offline.Read(o.Store) {
		out = append(out, queueItem{offline.ID(w), i, w.What, w.Method, w.Path, w.Key(), w.At})
	}
	return out
}

func init() {
	get("/queue", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		n := s.sites.rulesOf(s.ctx, s.opt).q
		n.mu.Lock()
		defer n.mu.Unlock()
		return map[string]any{"Items": queueList(s.opt), "Failed": append([]queueFailure{}, n.failed...), "Conflict": n.conflict}, nil
	})
	// Send now; Force goes over changes made in Jira since.
	post("/queue/send", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Force bool }](r)
		if err != nil {
			return nil, err
		}
		res := offline.Replay(ctx, s.Client(), s.opt.Store, b.Force)
		s.sites.rulesOf(s.ctx, s.opt).q.record(res)
		out := map[string]any{"Sent": res.Sent, "Failed": res.Failed, "Conflict": res.Conflict, "Left": res.Left}
		if res.Err != nil {
			out["Offline"] = jira.Offline(res.Err)
			out["Error"] = res.Err.Error()
		}
		return out, nil
	})
	del("/queue/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		id := r.PathValue("id")
		if !strings.HasPrefix(id, "w") {
			return nil, badRequest("bad id")
		}
		return map[string]int{"Left": offline.DropID(s.opt.Store, id)}, nil
	})
}
