package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/cli"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// Coding agents: every herdr agent, live over Server-Sent Events, with the
// issue each works on. The feature hides itself when herdr isn't running.

// herdrClient finds the running herdr server, nil for none; tests swap it.
var herdrClient = herdr.Default

// herdrBin is the herdr CLI, for reading a pane and focusing it.
var herdrBin = "herdr"

// AgentOut is one agent as the browser gets it.
type AgentOut struct {
	PaneID, Name, WorkspaceID, TabID, Agent string
	Status                                  herdr.Status
	CWD, Title                              string
	Focused                                 bool
	Key                                     string // the issue it works on, "" for none
}

// AgentsSnapshot is everything the agents screen and the cards need.
type AgentsSnapshot struct {
	Available bool
	Agents    []AgentOut
	Worktrees map[string]string // issue key → its linked worktree
}

func agentKey(a herdr.Agent) string {
	if k := work.BranchKey(a.Name); k != "" {
		return k
	}
	return work.BranchKey(filepath.Base(a.CWD))
}

func init() {
	get("/agents/status", agentsStatus)
	get("/agents", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return snapshot(ctx, s), nil
	})
	handle("GET /api/agents/events", agentEvents)
	get("/agents/{pane}/output", agentOutput)
	post("/agents/{pane}/prompt", agentPrompt)
	post("/agents/{pane}/focus", agentFocus)
	post("/agents/{pane}/stop", agentStop)
	post("/agents/{pane}/new", agentNew)
	get("/agents/issues", agentIssues)
}

// AgentIssue is an agent's issue on the first site that has it, the shown
// one first; Found is false when none does.
type AgentIssue struct {
	Card      jira.Card
	Site, URL string
	Found     bool
}

// agentIssues: ?keys=A-1,B-2, as the TUI's lookup: one key at a time, since
// a search naming a key a site lacks fails as a whole.
func agentIssues(ctx context.Context, s *Server, r *http.Request) (any, error) {
	var keys []string
	for k := range strings.SplitSeq(Q(r, "keys"), ",") {
		if k = strings.ToUpper(strings.TrimSpace(k)); jira.ValidKey(k) && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if len(keys) > 50 {
		keys = keys[:50]
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sites := s.siteClients()
	got := make([][]AgentIssue, len(keys))
	var wg sync.WaitGroup
	for i, k := range keys {
		got[i] = make([]AgentIssue, len(sites))
		for j, sc := range sites {
			wg.Go(func() {
				if cards, err := sc.Client.SearchCards(ctx, "key = "+k); err == nil && len(cards) == 1 {
					got[i][j] = AgentIssue{Card: cards[0], Site: sc.Site, URL: sc.Client.BrowseURL(k), Found: true}
				}
			})
		}
	}
	wg.Wait()
	out := map[string]AgentIssue{}
	for i, k := range keys {
		for _, is := range got[i] {
			if is.Found {
				out[k] = is
				break
			}
		}
		if !out[k].Found {
			out[k] = AgentIssue{}
		}
	}
	return map[string]any{"issues": out, "site": s.opt.Site}, nil
}

func agentsStatus(ctx context.Context, s *Server, r *http.Request) (any, error) {
	_, cliErr := exec.LookPath(herdrBin)
	return map[string]any{
		"Available": herdrClient() != nil,
		"CLI":       cliErr == nil,
		"GH":        cli.Have("gh"),
		"GLab":      cli.Have("glab"),
	}, nil
}

var (
	wtMu    sync.Mutex
	wtCache = map[string]wtEntry{} // by site
)

// worktreesByKey maps issue keys to their linked worktrees in the
// jira.repos checkouts, cached for a few seconds.
func worktreesByKey(s *Server) map[string]string {
	wtMu.Lock()
	defer wtMu.Unlock()
	if e, ok := wtCache[s.opt.Site]; ok && time.Since(e.at) < 5*time.Second {
		return e.m
	}
	out := map[string]string{}
	for _, repo := range s.opt.Jira.Repos {
		for path, branch := range linkedWorktrees(expandUserPath(repo)) {
			if k := work.BranchKey(branch); k != "" {
				out[k] = path
			}
		}
	}
	wtCache[s.opt.Site] = wtEntry{out, time.Now()}
	return out
}

type wtEntry struct {
	m  map[string]string
	at time.Time
}

func forgetWorktrees() { wtMu.Lock(); clear(wtCache); wtMu.Unlock() }

func toOut(as []herdr.Agent) []AgentOut {
	out := make([]AgentOut, 0, len(as))
	for _, a := range as {
		out = append(out, AgentOut{
			PaneID: a.PaneID, Name: a.Name, WorkspaceID: a.WorkspaceID, TabID: a.TabID, Agent: a.Agent,
			Status: a.Status, CWD: a.CWD, Title: a.Title, Focused: a.Focused, Key: agentKey(a),
		})
	}
	return out
}

func unavailable() AgentsSnapshot {
	return AgentsSnapshot{Agents: []AgentOut{}, Worktrees: map[string]string{}}
}

func snapshot(ctx context.Context, s *Server) AgentsSnapshot {
	c := herdrClient()
	if c == nil {
		return unavailable()
	}
	as, err := c.Agents(ctx)
	if err != nil {
		return unavailable()
	}
	return AgentsSnapshot{Available: true, Agents: toOut(as), Worktrees: worktreesByKey(s)}
}

// agentEvents streams a snapshot whenever the agents change: herdr's
// subscription wakes it, a slow tick catches what it doesn't push (titles).
func agentEvents(s *Server, w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ctx := r.Context()
	var last string
	send := func(snap AgentsSnapshot) {
		b, _ := json.Marshal(snap)
		if string(b) == last {
			return
		}
		last = string(b)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	sleep := func(d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()
	for ctx.Err() == nil {
		c := herdrClient()
		if c == nil {
			send(unavailable())
			if !sleep(10 * time.Second) {
				return
			}
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		as, err := c.Agents(lctx)
		cancel()
		if err != nil {
			send(unavailable())
			if !sleep(5 * time.Second) {
				return
			}
			continue
		}
		send(AgentsSnapshot{Available: true, Agents: toOut(as), Worktrees: worktreesByKey(s)})
		panes := paneIDs(as)
		st, err := c.Subscribe(ctx, panes)
		if err != nil {
			if !sleep(3 * time.Second) {
				return
			}
			continue
		}
		wake := make(chan struct{}, 1)
		go func() {
			defer close(wake)
			for {
				if _, err := st.Next(); err != nil {
					return
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}()
		resub := false
		tick := time.NewTicker(10 * time.Second)
	loop:
		for !resub {
			select {
			case <-ctx.Done():
				break loop
			case _, open := <-wake:
				if !open {
					break loop
				}
				if !sleep(250 * time.Millisecond) {
					break loop
				}
			case <-tick.C:
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
			}
			lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			as, err := c.Agents(lctx)
			cancel()
			if err != nil {
				break loop
			}
			send(AgentsSnapshot{Available: true, Agents: toOut(as), Worktrees: worktreesByKey(s)})
			resub = !slices.Equal(panes, paneIDs(as))
		}
		tick.Stop()
		st.Close()
		if ctx.Err() == nil && !resub {
			if !sleep(2 * time.Second) {
				return
			}
		}
	}
}

func paneIDs(as []herdr.Agent) []string {
	ids := make([]string, len(as))
	for i, a := range as {
		ids[i] = a.PaneID
	}
	slices.Sort(ids)
	return ids
}

func needHerdr() (*herdr.Client, error) {
	c := herdrClient()
	if c == nil {
		return nil, httpError{http.StatusServiceUnavailable, i18n.T("herdr is not running")}
	}
	return c, nil
}

// findAgent is the agent in pane.
func findAgent(ctx context.Context, c *herdr.Client, pane string) (herdr.Agent, error) {
	as, err := c.Agents(ctx)
	if err != nil {
		return herdr.Agent{}, err
	}
	for _, a := range as {
		if a.PaneID == pane {
			return a, nil
		}
	}
	return herdr.Agent{}, httpError{http.StatusNotFound, i18n.T("that agent is gone")}
}

// paneAgent is the agent in the request's {pane}: unknown panes and values
// that look like flags never reach the herdr CLI.
func paneAgent(ctx context.Context, c *herdr.Client, r *http.Request) (herdr.Agent, error) {
	pane := r.PathValue("pane")
	if pane == "" || strings.HasPrefix(pane, "-") {
		return herdr.Agent{}, badRequest(i18n.T("bad pane"))
	}
	return findAgent(ctx, c, pane)
}

// agentOutput is the pane's recent terminal text, read through the herdr CLI.
func agentOutput(ctx context.Context, s *Server, r *http.Request) (any, error) {
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	a, err := paneAgent(ctx, c, r)
	if err != nil {
		return nil, err
	}
	lines := 80
	if n, err := strconv.Atoi(Q(r, "lines")); err == nil && n > 0 && n <= 1000 {
		lines = n
	}
	bin, err := exec.LookPath(herdrBin)
	if err != nil {
		return nil, httpError{http.StatusNotImplemented, i18n.T("no herdr on PATH to read the terminal")}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "pane", "read", a.PaneID, "--source", "recent", "--lines", strconv.Itoa(lines), "--format", "text")
	cmd.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+c.Path())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("herdr pane read: %s", cli.Error(err))
	}
	return map[string]string{"Text": string(out)}, nil
}

func agentPrompt(ctx context.Context, s *Server, r *http.Request) (any, error) {
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	b, err := Body[struct{ Text string }](r)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(b.Text)
	if text == "" {
		return nil, badRequest(i18n.T("empty prompt"))
	}
	a, err := paneAgent(ctx, c, r)
	if err != nil {
		return nil, err
	}
	err = c.Prompt(ctx, a.PaneID, text)
	if herdr.IsCode(err, "agent_blocked") {
		return nil, httpError{http.StatusConflict, i18n.T("the agent waits on an approval or question: answer it in its terminal")}
	}
	return nil, err
}

// agentFocus brings the pane to the front in herdr.
func agentFocus(ctx context.Context, s *Server, r *http.Request) (any, error) {
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	a, err := paneAgent(ctx, c, r)
	if err != nil {
		return nil, err
	}
	bin, err := exec.LookPath(herdrBin)
	if err != nil {
		return nil, httpError{http.StatusNotImplemented, i18n.T("no herdr on PATH")}
	}
	cmd := exec.CommandContext(ctx, bin, "agent", "focus", a.PaneID)
	cmd.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+c.Path())
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("herdr agent focus: %s", strings.TrimSpace(string(out)))
	}
	return nil, nil
}

// agentStop closes the agent's tab.
func agentStop(ctx context.Context, s *Server, r *http.Request) (any, error) {
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	a, err := paneAgent(ctx, c, r)
	if err != nil {
		return nil, err
	}
	return nil, c.CloseTab(ctx, a.TabID)
}

// agentNew starts another agent in the same directory as the one in pane.
func agentNew(ctx context.Context, s *Server, r *http.Request) (any, error) {
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	b, err := Body[struct{ Agent, Prompt string }](r)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(b.Prompt), "-") {
		return nil, badRequest(i18n.T("prompt must not start with -"))
	}
	a, err := paneAgent(ctx, c, r)
	if err != nil {
		return nil, err
	}
	key := agentKey(a)
	kind := b.Agent
	if kind == "" {
		kind = workConfigOf(s).Agent
	} else if !slices.Contains(agentKinds, kind) {
		return nil, badRequest(i18n.Tf("unknown agent %s", kind))
	}
	_, pane, err := c.NewTab(ctx, a.WorkspaceID, cmpOr(key, a.Name), a.CWD, nil)
	if err != nil {
		return nil, err
	}
	args := workArgs(workConfigOf(s).Args, b.Prompt, key)
	if err := startAgent(ctx, c, kind, agentName(key, time.Now()), pane, args); err != nil {
		return nil, err
	}
	return map[string]string{"Pane": pane}, nil
}
