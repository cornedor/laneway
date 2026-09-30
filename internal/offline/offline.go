// Package offline keeps the writes that never reached Jira in the state
// file and sends them again; the TUI and the web UI share it.
package offline

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// Meta is where the queue lives in the state file.
const Meta = "jira_tab:queue"

// Every is how often a queue is retried.
const Every = 30 * time.Second

// mu guards the stored queue: the client adds to it from commands.
var mu sync.Mutex

// Read is the queued writes, oldest first.
func Read(st *store.Store) []jira.PendingWrite {
	if st == nil {
		return nil
	}
	v, _, _ := st.GetMeta(Meta)
	var ws []jira.PendingWrite
	_ = json.Unmarshal([]byte(v), &ws)
	return ws
}

// Write replaces the queue.
func Write(st *store.Store, ws []jira.PendingWrite) {
	if len(ws) == 0 {
		_ = st.DeleteMeta(Meta)
		return
	}
	b, _ := json.Marshal(ws)
	_ = st.SetMeta(Meta, string(b))
}

// To is the client's queue: the state file.
func To(st *store.Store) func(jira.PendingWrite) {
	return func(w jira.PendingWrite) {
		mu.Lock()
		defer mu.Unlock()
		Write(st, append(Read(st), w))
	}
}

// Result is what a Replay did.
type Result struct {
	Sent     int
	Failed   []string // writes Jira refused, dropped
	Conflict string   // the issue that changed since, where it stopped
	Left     int
	Err      error
}

// Replay sends the queued writes, oldest first; force skips the check that
// the issue hasn't changed since.
func Replay(ctx context.Context, c *jira.Client, st *store.Store, force bool) Result {
	mu.Lock()
	defer mu.Unlock()
	ws := Read(st)
	var res Result
	for len(ws) > 0 {
		w := ws[0]
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if !force {
			changed, err := c.ChangedSince(cctx, w.Key(), w.At)
			if err != nil { // offline still, or unreadable: try later
				cancel()
				res.Err = err
				break
			}
			if changed {
				cancel()
				res.Conflict = w.Key()
				break
			}
		}
		err := c.Replay(cctx, w)
		cancel()
		if errors.Is(err, jira.ErrQueued) {
			break
		}
		if err != nil {
			res.Failed = append(res.Failed, w.What+": "+err.Error())
		} else {
			res.Sent++
		}
		ws, force = ws[1:], false
	}
	Write(st, ws)
	res.Left = len(ws)
	return res
}

// Drop removes write i, and says how many are left.
func Drop(st *store.Store, i int) int {
	mu.Lock()
	defer mu.Unlock()
	ws := Read(st)
	if i >= 0 && i < len(ws) {
		ws = append(ws[:i], ws[i+1:]...)
		Write(st, ws)
	}
	return len(ws)
}
