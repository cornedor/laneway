package rules

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// Watcher runs a Set over its watches' searches, like the TUI does while
// it is open: log, notify, exec, transition and comment; highlight goes to
// Highlight, for a board to mark. Out and the log are shared by the watches.
type Watcher struct {
	C   *jira.Client
	Set *Set
	Log string    // rules.log
	Out io.Writer // one line per firing or error; nil for none
	// Notify gets each notify firing; nil skips them.
	Notify func(Firing)
	// Fired gets each action taken (not highlight) with its log line, and
	// the error when it failed.
	Fired func(f Firing, line string, err error)
	// Highlight gets each highlight firing; nil skips them.
	Highlight func(Firing)
	mu        sync.Mutex
}

// Run polls every watch until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, wt := range w.Set.Watches() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Loop(ctx, wt)
		}()
	}
	wg.Wait()
}

// Loop polls one watch every wt.Every. The first search is the baseline.
func (w *Watcher) Loop(ctx context.Context, wt Watch) {
	var prev []jira.Card
	seen := false
	for {
		cards, err := w.C.SearchCards(ctx, wt.JQL)
		switch {
		case err != nil && ctx.Err() == nil:
			w.Print(fmt.Sprintf("%s: %v\n", wt.JQL, err))
		case err == nil && seen:
			w.Fire(ctx, wt.JQL, prev, cards)
		}
		if err == nil {
			prev, seen = cards, true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wt.Every):
		}
	}
}

// Fire runs the actions of every rule the changes from prev to cur fire.
func (w *Watcher) Fire(ctx context.Context, jql string, prev, cur []jira.Card) {
	events := Diff(prev, cur)
	for i := range events {
		events[i].Watch = jql
	}
	if len(events) > 0 && w.Set.UsesByMe() {
		if err := ResolveByMe(ctx, w.C, events, ""); err != nil {
			w.Print("by_me: " + err.Error() + "\n")
		}
	}
	now := time.Now()
	var lines []string
	for _, ev := range events {
		for _, f := range w.Set.Fire(ev) {
			line := LogLine(now, f)
			var err error
			switch f.Action {
			case "log":
				lines = append(lines, line)
			case "notify":
				if w.Notify != nil {
					w.Notify(f)
				}
			case "exec":
				err = Exec(ctx, f)
			case "transition", "comment":
				err = JiraAct(ctx, w.C, f)
			case "highlight":
				if w.Highlight != nil {
					w.Highlight(f)
				}
				continue
			}
			if err != nil {
				line = err.Error() + "\n"
			}
			w.Print(line)
			if w.Fired != nil {
				w.Fired(f, line, err)
			}
		}
	}
	if len(lines) > 0 {
		if err := AppendLog(w.Log, lines); err != nil {
			w.Print("log: " + err.Error() + "\n")
		}
	}
}

// Print writes to Out.
func (w *Watcher) Print(s string) {
	if w.Out == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprint(w.Out, s)
}
