package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// The rules engine runs inside the server as in the TUI: watches are polled
// on a ticker, and board reads are diffed against the last one seen. What
// fired goes to rules.log (log actions), to the feed below (every action)
// and from there to the browser, which shows notify actions as notifications.

// RuleEvent is one action a rule took.
type RuleEvent struct {
	ID     int
	Time   time.Time
	Rule   string
	Action string
	Title  string
	Text   string
	Key    string
	Color  string // highlight: ANSI 0–255 or #rrggbb, "" for the theme's
	Err    string
}

const feedSize = 200

type ruleFeed struct {
	mu     sync.Mutex
	events []RuleEvent
	next   int
	subs   map[chan RuleEvent]struct{}
}

func (f *ruleFeed) add(ev RuleEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	ev.ID, ev.Time = f.next, time.Now()
	f.events = append(f.events, ev)
	if len(f.events) > feedSize {
		f.events = f.events[len(f.events)-feedSize:]
	}
	for c := range f.subs {
		select {
		case c <- ev:
		default:
		}
	}
}

func (f *ruleFeed) since(id int) []RuleEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []RuleEvent
	for _, e := range f.events {
		if e.ID > id {
			out = append(out, e)
		}
	}
	return out
}

func (f *ruleFeed) subscribe() (chan RuleEvent, func()) {
	c := make(chan RuleEvent, 32)
	f.mu.Lock()
	if f.subs == nil {
		f.subs = map[chan RuleEvent]struct{}{}
	}
	f.subs[c] = struct{}{}
	f.mu.Unlock()
	return c, func() { f.mu.Lock(); delete(f.subs, c); f.mu.Unlock() }
}

// ruleRunner is one site's rules.
type ruleRunner struct {
	opt  Options
	set  *rules.Set
	warn []string
	feed *ruleFeed
	w    *rules.Watcher
	log  string
	mu   sync.Mutex
	prev *prevCache // board reads by view
	q    *queueNotes
}

// prevCache keeps the last board read of the most recent views only.
type prevCache struct {
	cap   int
	order []string // least recently used first
	m     map[string][]jira.Card
}

func newPrevCache(n int) *prevCache { return &prevCache{cap: n, m: map[string][]jira.Card{}} }

func (c *prevCache) len() int { return len(c.m) }

func (c *prevCache) touch(k string) {
	if i := slices.Index(c.order, k); i >= 0 {
		c.order = slices.Delete(c.order, i, i+1)
	}
	c.order = append(c.order, k)
}

func (c *prevCache) get(k string) ([]jira.Card, bool) {
	v, ok := c.m[k]
	if ok {
		c.touch(k)
	}
	return v, ok
}

func (c *prevCache) put(k string, v []jira.Card) {
	c.m[k] = v
	c.touch(k)
	for len(c.order) > c.cap {
		delete(c.m, c.order[0])
		c.order = c.order[1:]
	}
}

// ruleLogPath is rules.log beside the state file, as the TUI keeps it.
func ruleLogPath(o Options) string {
	if o.Store == nil || o.Store.Path() == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(o.Store.Path()), "rules.log")
}

// rulesOf is the site's runner, made and started on first use.
func (ss *siteSet) rulesOf(ctx context.Context, o Options) *ruleRunner {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if r, ok := ss.rules[o.Site]; ok {
		return r
	}
	set, warn := rules.Compile(o.Rules)
	r := &ruleRunner{opt: o, set: set, warn: warn, feed: &ruleFeed{}, log: ruleLogPath(o), prev: newPrevCache(64)}
	r.q = &queueNotes{feed: r.feed}
	r.w = &rules.Watcher{C: o.Client, Set: set, Log: r.log, Fired: r.fired, Highlight: r.highlight}
	if ss.rules == nil {
		ss.rules = map[string]*ruleRunner{}
	}
	ss.rules[o.Site] = r
	if len(set.Watches()) > 0 && o.Client != nil {
		go r.w.Run(ctx)
	}
	go runQueue(ctx, o, r.q)
	return r
}

// highlight puts a highlight on the stream: the page marks the card ● until
// it is opened, as the TUI does.
func (r *ruleRunner) highlight(f rules.Firing) {
	r.feed.add(RuleEvent{Rule: f.Rule, Action: f.Action, Key: f.Vars["Key"], Color: f.Color})
}

func (r *ruleRunner) fired(f rules.Firing, _ string, err error) {
	ev := RuleEvent{Rule: f.Rule, Action: f.Action, Title: f.Title, Text: f.Text, Key: f.Vars["Key"]}
	if err != nil {
		ev.Err = err.Error()
	}
	r.feed.add(ev)
}

// observe diffs a board read against the last one of the same view and fires
// the rules the changes match; the first read of a view is its baseline.
func (r *ruleRunner) observe(ctx context.Context, view string, cards []jira.Card) {
	if r.set.Len() == 0 {
		return
	}
	r.mu.Lock()
	prev, seen := r.prev.get(view)
	r.prev.put(view, cards)
	r.mu.Unlock()
	if seen {
		go r.w.Fire(ctx, "", prev, cards)
	}
}

// observeRules is called by the board handler with the cards it read.
func observeRules(s *Server, r *http.Request, cards []jira.Card) {
	rr := s.sites.rulesOf(s.ctx, s.opt)
	rr.observe(s.ctx, r.URL.Path+"?"+r.URL.Query().Encode(), cards)
}

func init() {
	get("/rules", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		rr := s.sites.rulesOf(s.ctx, s.opt)
		counts := map[string]int{}
		for _, e := range rr.feed.since(0) {
			counts[e.Rule]++
		}
		type watch struct {
			JQL   string
			Every string
		}
		ws := []watch{}
		for _, w := range rr.set.Watches() {
			ws = append(ws, watch{w.JQL, w.Every.String()})
		}
		last := 0
		if evs := rr.feed.since(0); len(evs) > 0 {
			last = evs[len(evs)-1].ID
		}
		return map[string]any{
			"Rules": rr.set.Rules(), "Warnings": nonNil(rr.warn), "Watches": ws, "Counts": counts,
			"Test":  map[string]string{"Type": s.opt.RulesTest.Type, "Status": s.opt.RulesTest.Status},
			"Kinds": []string{rules.New, rules.Status, rules.Assignee, rules.Priority, rules.Points, rules.Summary},
			"Last":  last, "Running": len(ws) > 0, "Log": rr.log,
		}, nil
	})
	post("/rules/test", ruleTest)
	get("/rules/log", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		n, _ := strconv.Atoi(Q(r, "n"))
		if n <= 0 || n > 1000 {
			n = 200
		}
		return map[string]any{"Lines": tailLines(ruleLogPath(s.opt), n)}, nil
	})
	get("/rules/events", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		id, _ := strconv.Atoi(Q(r, "since"))
		return s.sites.rulesOf(s.ctx, s.opt).feed.since(id), nil
	})
	handle("GET /api/rules/stream", ruleStream)
}

// ruleStream is a server-sent event stream of RuleEvents; Last-Event-ID or
// ?since= resumes.
func ruleStream(s *Server, w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	rr := s.sites.rulesOf(s.ctx, s.opt)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	since, _ := strconv.Atoi(cmpStr(r.Header.Get("Last-Event-ID"), Q(r, "since")))
	ch, off := rr.feed.subscribe()
	defer off()
	send := func(e RuleEvent) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.ID, b)
	}
	fmt.Fprint(w, "retry: 5000\n\n")
	for _, e := range rr.feed.since(since) {
		send(e)
		since = e.ID
	}
	fl.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case e := <-ch:
			if e.ID > since {
				send(e)
				since = e.ID
			}
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// tailLines is the last n lines of the file, newest last.
func tailLines(path string, n int) []string {
	out := []string{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
		if len(out) > 2*n {
			out = append([]string(nil), out[len(out)-n:]...)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

type ruleTestIn struct {
	On, Key, Summary, Type, Status, FromStatus, Assignee, Priority, Points, Watch string
	ByMe                                                                          string // "true", "false" or "" (unknown)
}

// ruleTest says which rules a described change would fire and what stopped
// the rest; nothing runs (`laneway rules test`).
func ruleTest(ctx context.Context, s *Server, r *http.Request) (any, error) {
	in, err := Body[ruleTestIn](r)
	if err != nil {
		return nil, err
	}
	if in.On == "" {
		in.On = rules.New
	}
	if in.Key == "" {
		in.Key = "TEST-1"
	}
	if in.Priority == "" {
		in.Priority = "Medium"
	}
	typ, status := s.opt.RulesTest.Type, s.opt.RulesTest.Status
	c := jira.Card{Key: in.Key, Summary: in.Summary, Type: cmpStr(in.Type, cmpStr(typ, "Task")),
		Status: cmpStr(in.Status, cmpStr(status, "To Do")), Assignee: in.Assignee, Priority: in.Priority, Points: in.Points}
	if c.Assignee != "" {
		c.AssigneeID = "test"
	}
	ev := rules.Event{Kind: in.On, Card: c, Old: c, Watch: in.Watch}
	ev.Old.Status = in.FromStatus
	if in.ByMe != "" {
		b := in.ByMe == "true"
		ev.ByMe = &b
	}
	if in.On == rules.New {
		ev.Old = jira.Card{}
	}
	set := s.sites.rulesOf(s.ctx, s.opt).set
	type act struct{ Type, Text, Note string }
	type res struct {
		Rule    string
		Fires   bool
		Why     string
		Actions []act
	}
	fired := set.Fire(ev)
	all := set.Rules()
	var out []res
	for i, x := range set.Explain(ev) {
		row := res{Rule: x.Rule, Why: x.Text, Actions: []act{}}
		if x.Text == "" {
			row.Fires = true
			for len(fired) > 0 && fired[0].Rule == x.Rule {
				f := fired[0]
				fired = fired[1:]
				row.Actions = append(row.Actions, act{Type: f.Action, Text: describeFiring(f)})
			}
			if ev.ByMe == nil || *ev.ByMe {
				for _, a := range all[i].Actions {
					if rules.JiraAction(a.Type) {
						row.Actions = append(row.Actions, act{Type: a.Type, Note: i18n.T("only on others' changes (by_me false)")})
					}
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// describeFiring is what a fired action would do.
func describeFiring(f rules.Firing) string {
	switch f.Action {
	case "notify":
		return f.Title + ": " + f.Text
	case "exec":
		return strings.Join(f.Argv, " ")
	case "transition":
		return "→ " + f.To
	case "highlight":
		if f.Color != "" {
			return f.Color
		}
		return i18n.T("theme highlight")
	}
	return f.Text
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
