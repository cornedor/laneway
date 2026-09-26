package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// rulesWatch polls the rules' watches until interrupted, like the TUI does,
// running log, notify and exec. highlight needs a board and is skipped.
func rulesWatch(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("rules watch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	cfgPath := fs.String("config", "", "config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, _, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	set, warn := rules.Compile(cfg.Rules)
	for _, w := range warn {
		fmt.Fprintln(errOut, "skipped:", w)
	}
	watches := set.Watches()
	if len(watches) == 0 {
		fmt.Fprintln(errOut, "laneway: no rule has a watch:")
		return 1
	}
	if cfg.Jira.BaseURL == "" || cfg.Jira.Email == "" || cfg.Jira.APIToken == "" {
		fmt.Fprintln(errOut, "laneway: jira.base_url, jira.email and jira.api_token (or JIRA_API_TOKEN) must be set")
		return 1
	}
	state, err := config.StatePath()
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	timeout, err := cfg.Jira.RequestTimeout()
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	c := jira.New(jira.Config{BaseURL: cfg.Jira.BaseURL, Email: cfg.Jira.Email, APIToken: cfg.Jira.APIToken,
		StoryPointsField: cfg.Jira.StoryPointsField, CardLimit: cfg.UI.CardLimit, Timeout: timeout})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	st, _ := os.Stdout.Stat()
	w := &watcher{c: c, set: set, log: filepath.Join(filepath.Dir(state), "rules.log"), out: out,
		notify: out == io.Writer(os.Stdout) && st != nil && st.Mode()&os.ModeCharDevice != 0}
	var wg sync.WaitGroup
	for _, wt := range watches {
		fmt.Fprintf(out, "watching %s every %s\n", wt.JQL, wt.Every)
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx, wt)
		}()
	}
	wg.Wait()
	return 0
}

// watcher runs the rules over each watch's searches; out and the log are
// shared by the watches.
type watcher struct {
	c      *jira.Client
	set    *rules.Set
	log    string
	out    io.Writer
	notify bool // out is a terminal: notify as OSC 777
	mu     sync.Mutex
}

func (w *watcher) loop(ctx context.Context, wt rules.Watch) {
	var prev []jira.Card
	seen := false
	for {
		cards, err := w.c.SearchCards(ctx, wt.JQL)
		switch {
		case err != nil && ctx.Err() == nil:
			w.print(fmt.Sprintf("%s: %v\n", wt.JQL, err))
		case err == nil && seen:
			w.fire(ctx, wt.JQL, prev, cards)
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

// fire runs the actions of every rule the changes from prev to cur fire.
func (w *watcher) fire(ctx context.Context, jql string, prev, cur []jira.Card) {
	events := rules.Diff(prev, cur)
	for i := range events {
		events[i].Watch = jql
	}
	if len(events) > 0 && w.set.UsesByMe() {
		if err := rules.ResolveByMe(ctx, w.c, events, ""); err != nil {
			w.print("by_me: " + err.Error() + "\n")
		}
	}
	now := time.Now()
	var lines []string
	for _, ev := range events {
		for _, f := range w.set.Fire(ev) {
			line := rules.LogLine(now, f)
			switch f.Action {
			case "log":
				lines = append(lines, line)
			case "notify":
				if w.notify {
					w.print(rules.NotifySeq(f.Title, f.Text))
				}
			case "exec":
				if err := rules.Exec(ctx, f); err != nil {
					line = err.Error() + "\n"
				}
			case "transition", "comment":
				if err := rules.JiraAct(ctx, w.c, f); err != nil {
					line = err.Error() + "\n"
				}
			case "highlight":
				continue
			}
			w.print(line)
		}
	}
	if len(lines) > 0 {
		if err := rules.AppendLog(w.log, lines); err != nil {
			w.print("log: " + err.Error() + "\n")
		}
	}
}

func (w *watcher) print(s string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprint(w.out, s)
}
