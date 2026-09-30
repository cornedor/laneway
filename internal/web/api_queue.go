package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/offline"
)

// Writes that never reached Jira wait in the state file (internal/offline),
// as in the TUI, and are retried every offline.Every while the server runs.

// runQueue retries the site's queue until ctx ends.
func runQueue(ctx context.Context, o Options) {
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
			offline.Replay(ctx, o.Client, o.Store, false)
		}
	}
}

type queueItem struct {
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
		out = append(out, queueItem{i, w.What, w.Method, w.Path, w.Key(), w.At})
	}
	return out
}

func init() {
	get("/queue", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return queueList(s.opt), nil
	})
	// Send now; Force goes over changes made in Jira since.
	post("/queue/send", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Force bool }](r)
		if err != nil {
			return nil, err
		}
		res := offline.Replay(ctx, s.Client(), s.opt.Store, b.Force)
		out := map[string]any{"Sent": res.Sent, "Failed": res.Failed, "Conflict": res.Conflict, "Left": res.Left}
		if res.Err != nil {
			out["Offline"] = jira.Offline(res.Err)
			out["Error"] = res.Err.Error()
		}
		return out, nil
	})
	del("/queue/{index}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		i, err := strconv.Atoi(r.PathValue("index"))
		if err != nil {
			return nil, badRequest("bad index")
		}
		return map[string]int{"Left": offline.Drop(s.opt.Store, i)}, nil
	})
}
