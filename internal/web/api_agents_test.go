package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/cli"
	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/review"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/work"
)

// fakeHerdr answers the socket calls the web API makes.
type fakeHerdr struct {
	mu     sync.Mutex
	calls  []string
	params map[string]map[string]any // the last params of each method
	agents []map[string]any
	subs   []net.Conn
	open   bool // worktree.open finds the worktree
	// startFails: agent.start fails this many times first, always when < 0.
	startFails int
	createErr  string // worktree.create fails with it
}

func newFakeHerdr(t *testing.T) (*fakeHerdr, *herdr.Client) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no unix sockets:", err)
	}
	f := &fakeHerdr{agents: []map[string]any{{
		"pane_id": "p1", "name": "jira-demo-4-abc", "workspace_id": "w1", "tab_id": "t1", "agent": "claude",
		"agent_status": "working", "cwd": "/w/demo-4", "terminal_title_stripped": "thinking",
	}}}
	t.Cleanup(func() {
		ln.Close()
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, c := range f.subs {
			c.Close()
		}
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f, herdr.New(sock)
}

func (f *fakeHerdr) serve(conn net.Conn) {
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		conn.Close()
		return
	}
	var req struct {
		ID, Method string
		Params     map[string]any
	}
	_ = json.Unmarshal(line, &req)
	f.mu.Lock()
	f.calls = append(f.calls, req.Method)
	if f.params == nil {
		f.params = map[string]map[string]any{}
	}
	f.params[req.Method] = req.Params
	var result any = map[string]any{}
	var errObj any
	switch req.Method {
	case "agent.list":
		result = map[string]any{"agents": f.agents}
	case "worktree.open":
		if !f.open {
			errObj = map[string]any{"code": "worktree_not_found", "message": "no"}
		} else {
			result = worktree(true)
		}
	case "worktree.create":
		if f.createErr != "" {
			errObj = map[string]any{"code": "git_failed", "message": f.createErr}
			break
		}
		f.open = true
		result = worktree(false)
	case "agent.start":
		if f.startFails != 0 {
			f.startFails--
			errObj = map[string]any{"code": "pane_busy", "message": "the pane is busy"}
		}
	case "tab.create":
		result = map[string]any{"tab": map[string]any{"tab_id": "t3"}, "root_pane": map[string]any{"pane_id": "p3"}}
	case "agent.prompt":
		if req.Params["text"] == "block" {
			errObj = map[string]any{"code": "agent_blocked", "message": "blocked"}
		}
	case "events.subscribe":
		f.subs = append(f.subs, conn)
	}
	f.mu.Unlock()
	resp := map[string]any{"id": req.ID, "result": result}
	if errObj != nil {
		resp = map[string]any{"id": req.ID, "error": errObj}
	}
	b, _ := json.Marshal(resp)
	_, _ = conn.Write(append(b, '\n'))
	if req.Method != "events.subscribe" {
		conn.Close()
	}
}

func worktree(already bool) map[string]any {
	return map[string]any{
		"already_open": already, "workspace": map[string]any{"workspace_id": "w2"},
		"root_pane": map[string]any{"pane_id": "p2", "tab_id": "t2"}, "worktree": map[string]any{"path": "/w/demo-5"},
	}
}

func (f *fakeHerdr) called(m string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == m {
			return true
		}
	}
	return false
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skip("git:", err, string(out))
		}
	}
	return dir
}

func agentsServer(t *testing.T, repo string) *httptest.Server {
	t.Helper()
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	st, _ := store.Open(filepath.Join(t.TempDir(), "state.json"))
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	opt := Options{
		Client: cl, Store: st, Site: "demo",
		Jira: config.JiraConfig{Repos: map[string]string{"DEMO": repo}},
		UI:   config.UIConfig{StartAssigns: "on", StartStatus: "In Progress", TimerOnStart: "on"},
	}
	ts := httptest.NewServer(New(context.Background(), opt))
	t.Cleanup(ts.Close)
	return ts
}

func TestBranchKey(t *testing.T) {
	for in, want := range map[string]string{
		"issue/ABC-12-fix": "ABC-12", "abc-12": "ABC-12", "jira-abc-12-x1y": "ABC-12", "fix/utf-8": "UTF-8", "main": "", "worktree-webui": "",
	} {
		if got := work.BranchKey(in); got != want {
			t.Errorf("work.BranchKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentsUnavailable(t *testing.T) {
	old := herdrClient
	herdrClient = func() *herdr.Client { return nil }
	defer func() { herdrClient = old }()
	ts := agentsServer(t, t.TempDir())
	var st map[string]any
	workCall(t, "GET", ts.URL+"/api/agents/status", "", &st)
	if st["Available"] != false {
		t.Errorf("status = %v", st)
	}
	var snap AgentsSnapshot
	workCall(t, "GET", ts.URL+"/api/agents", "", &snap)
	if snap.Available || len(snap.Agents) != 0 {
		t.Errorf("snapshot = %+v", snap)
	}
	if c := workCall(t, "POST", ts.URL+"/api/agents/p1/prompt", `{"Text":"hi"}`, nil); c != http.StatusServiceUnavailable {
		t.Errorf("prompt without herdr = %d", c)
	}
}

func TestAgentsListPromptStart(t *testing.T) {
	f, c := newFakeHerdr(t)
	old := herdrClient
	herdrClient = func() *herdr.Client { return c }
	defer func() { herdrClient = old }()
	oldWait := startAgentWait
	startAgentWait = time.Millisecond
	defer func() { startAgentWait = oldWait }()
	repo := gitRepo(t)
	ts := agentsServer(t, repo)

	var snap AgentsSnapshot
	workCall(t, "GET", ts.URL+"/api/agents", "", &snap)
	if !snap.Available || len(snap.Agents) != 1 || snap.Agents[0].Key != "DEMO-4" || snap.Agents[0].Status != herdr.Working {
		t.Fatalf("snapshot = %+v", snap)
	}

	if code := workCall(t, "POST", ts.URL+"/api/agents/p1/prompt", `{"Text":"go on"}`, nil); code != 200 {
		t.Errorf("prompt = %d", code)
	}
	var e map[string]string
	if code := workCall(t, "POST", ts.URL+"/api/agents/p1/prompt", `{"Text":"block"}`, &e); code != http.StatusConflict {
		t.Errorf("blocked prompt = %d %v", code, e)
	}
	if code := workCall(t, "POST", ts.URL+"/api/agents/p1/prompt", `{"Text":" "}`, nil); code != 400 {
		t.Errorf("empty prompt = %d", code)
	}

	var form struct {
		Repo, Branch, Prompt string
		Actions              []string
	}
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form)
	if form.Repo != repo || !strings.HasPrefix(form.Branch, "issue/DEMO-5-") || !strings.Contains(form.Prompt, "DEMO-5") ||
		strings.Join(form.Actions, ",") != "assign to you,move to In Progress,start timer" {
		t.Errorf("form = %+v", form)
	}

	var res struct {
		Path, Pane string
		Running    bool
		Did        []string
		Timer      bool
		Warn       string
	}
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Agent":"claude","Branch":"issue/DEMO-5-x","Prompt":"hello","Actions":true}`, &res); code != 200 {
		t.Fatalf("start = %d", code)
	}
	if res.Pane != "p2" || res.Running || !res.Timer || res.Warn != "" || strings.Join(res.Did, ",") != "assigned to you,moved to In Progress" {
		t.Errorf("start = %+v", res)
	}
	for _, m := range []string{"worktree.open", "worktree.create", "tab.rename", "agent.start"} {
		if !f.called(m) {
			t.Errorf("herdr %s not called", m)
		}
	}
	var iss jira.Issue
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5?fresh=1", "", &iss)
	if iss.Status != "In Progress" {
		t.Errorf("status = %q", iss.Status)
	}

	f.mu.Lock()
	f.agents = append(f.agents, map[string]any{"pane_id": "p2", "workspace_id": "w2", "agent": "claude", "agent_status": "idle"})
	f.mu.Unlock()
	res.Running = false
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{}`, &res); code != 200 || !res.Running {
		t.Errorf("second start = %d %+v, want the running agent", code, res)
	}
}

func TestStartWorkNoRepo(t *testing.T) {
	_, c := newFakeHerdr(t)
	old := herdrClient
	herdrClient = func() *herdr.Client { return c }
	defer func() { herdrClient = old }()
	ts := agentsServer(t, "")
	var e map[string]string
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{}`, &e); code != 400 || !strings.Contains(e["error"], "jira.repos") {
		t.Errorf("no repo = %d %v", code, e)
	}
}

func TestAgentEvents(t *testing.T) {
	f, c := newFakeHerdr(t)
	old := herdrClient
	herdrClient = func() *herdr.Client { return c }
	defer func() { herdrClient = old }()
	ts := agentsServer(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/agents/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	sc := bufio.NewScanner(res.Body)
	next := func() AgentsSnapshot {
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var s AgentsSnapshot
				_ = json.Unmarshal([]byte(d), &s)
				return s
			}
		}
		t.Fatal("stream ended")
		return AgentsSnapshot{}
	}
	if s := next(); !s.Available || s.Agents[0].Status != herdr.Working {
		t.Fatalf("first = %+v", s)
	}
	// herdr pushes an event after the agent changed state.
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		f.agents[0]["agent_status"] = "blocked"
		subs := append([]net.Conn(nil), f.subs...)
		f.mu.Unlock()
		for _, sub := range subs {
			_, _ = sub.Write([]byte(`{"event":"pane.agent_status_changed","data":{"pane_id":"p1","agent_status":"blocked"}}` + "\n"))
		}
		if len(subs) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never subscribed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s := next(); s.Agents[0].Status != herdr.Blocked {
		t.Errorf("second = %+v", s)
	}
}

func TestReviewRoute(t *testing.T) {
	if !cli.Have("gh") && !cli.Have("glab") {
		t.Skip("no gh or glab")
	}
	old := review.Requests
	review.Requests = func(context.Context) ([]review.Request, error) { return []review.Request{{Title: "DEMO-4 x"}}, nil }
	defer func() { review.Requests = old }()
	ts := agentsServer(t, t.TempDir())
	var out struct {
		Keys  []string
		Cards []jira.Card
	}
	if code := workCall(t, "GET", ts.URL+"/api/review", "", &out); code != 200 || len(out.Cards) != 1 || out.Cards[0].Key != "DEMO-4" {
		t.Errorf("review = %d %+v", code, out)
	}
}

func TestBranchAndPullRequest(t *testing.T) {
	repo := gitRepo(t)
	if out, err := exec.Command("git", "-C", repo, "branch", "issue/DEMO-5-old").CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	ts := agentsServer(t, repo)
	var b map[string]string
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/branch", "", &b)
	if !strings.HasPrefix(b["Name"], "DEMO-5-") {
		t.Errorf("branch = %v", b)
	}
	var calls []string
	old := runIn
	runIn = func(_ context.Context, dir, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "git" && args[0] == "remote" {
			return "git@gitlab.example.com:x/y.git", nil
		}
		if name == "glab" {
			return "Creating draft\nhttps://gitlab.example.com/x/y/-/merge_requests/3", nil
		}
		return "", nil
	}
	defer func() { runIn = old }()
	var pr map[string]string
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/pr", "", &pr); code != 200 || pr["URL"] != "https://gitlab.example.com/x/y/-/merge_requests/3" || pr["Branch"] != "issue/DEMO-5-old" {
		t.Errorf("pr = %d %v calls %v", code, pr, calls)
	}
}

// The whole start sequence: form defaults, then worktree, tab label, agent with
// the {key} prompt, the writes; a second S finds the agent; "another" opens a tab.
func TestStartWorkSequence(t *testing.T) {
	f, c := newFakeHerdr(t)
	old := herdrClient
	herdrClient = func() *herdr.Client { return c }
	defer func() { herdrClient = old }()
	oldWait := startAgentWait
	startAgentWait = time.Millisecond
	defer func() { startAgentWait = oldWait }()
	ts := agentsServer(t, gitRepo(t))

	var form struct {
		Herdr   bool
		Agent   string
		Running []AgentOut
	}
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form)
	if !form.Herdr || form.Agent != "claude" || len(form.Running) != 0 {
		t.Fatalf("form = %+v", form)
	}
	var res struct {
		Pane    string
		Running bool
		Did     []string
	}
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Agent":"claude","Prompt":"Do {key} now","Actions":true}`, &res); code != 200 {
		t.Fatalf("start = %d", code)
	}
	f.mu.Lock()
	order := strings.Join(f.calls, ",")
	start := f.params["agent.start"]
	rename := f.params["tab.rename"]
	f.mu.Unlock()
	if !strings.Contains(order, "worktree.open,worktree.create,tab.rename,agent.start") {
		t.Errorf("calls = %s", order)
	}
	if fmt.Sprint(start["args"]) != "[Do DEMO-5 now]" || start["pane_id"] != "p2" || start["kind"] != "claude" {
		t.Errorf("agent.start = %v", start)
	}
	if fmt.Sprint(rename["label"]) != "DEMO-5" {
		t.Errorf("tab.rename = %v", rename)
	}
	if strings.Join(res.Did, ",") != "assigned to you,moved to In Progress" {
		t.Errorf("did = %v", res.Did)
	}

	// Its agent runs now: the form says so, S does not start a second one.
	f.mu.Lock()
	f.agents = append(f.agents, map[string]any{"pane_id": "p2", "name": "jira-demo-5-x", "workspace_id": "w2", "tab_id": "t2", "agent": "claude", "agent_status": "idle", "cwd": "/w/demo-5"})
	f.calls = nil
	f.mu.Unlock()
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form)
	if len(form.Running) != 1 || form.Running[0].PaneID != "p2" {
		t.Errorf("running = %+v", form.Running)
	}
	workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{}`, &res)
	if !res.Running || res.Pane != "p2" || f.called("agent.start") {
		t.Errorf("second start = %+v, started: %v", res, f.called("agent.start"))
	}

	// Another agent: a new tab in the worktree, no writes.
	res.Running = true
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Another":true,"Prompt":"more","Actions":true}`, &res); code != 200 || res.Running || res.Pane != "p3" || len(res.Did) != 0 {
		t.Fatalf("another = %d %+v", code, res)
	}
	f.mu.Lock()
	tab, start := f.params["tab.create"], f.params["agent.start"]
	f.mu.Unlock()
	if tab["workspace_id"] != "w2" || tab["cwd"] != "/w/demo-5" || start["pane_id"] != "p3" {
		t.Errorf("tab.create %v agent.start %v", tab, start)
	}
}

func TestStartWorkErrorsSayHow(t *testing.T) {
	if e := startError("DEMO-5", "/r", fmt.Errorf("fatal: 'issue/x' is already checked out")); !strings.Contains(e.Error(), "another branch") {
		t.Errorf("taken branch: %v", e)
	}
	if !strings.Contains(noRepo("DEMO"), "jira.repos.DEMO") {
		t.Error(noRepo("DEMO"))
	}
}

// The cwd's branch names the palette's first issue, only one that exists.
func TestCwdBranchIssue(t *testing.T) {
	ts := agentsServer(t, t.TempDir())
	old := runIn
	defer func() { runIn = old }()
	for branch, want := range map[string]string{"issue/demo-5-fix": "DEMO-5", "fix/utf-8": "", "DEMO-9999-gone": "", "": ""} {
		runIn = func(context.Context, string, string, ...string) (string, error) { return branch, nil }
		var b map[string]string
		if workCall(t, "GET", ts.URL+"/api/branch", "", &b) != 200 || b["Key"] != want || want != "" && b["Summary"] == "" {
			t.Errorf("%q: branch = %v, want %q", branch, b, want)
		}
	}
}

// An agent's issue is looked up on the shown site first, then the others;
// a key no site has is not found.
func TestAgentIssuesAcrossSites(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	gone := httptest.NewServer(http.NotFoundHandler()) // the shown site knows no issue
	t.Cleanup(gone.Close)
	st, _ := store.Open(filepath.Join(t.TempDir(), "state.json"))
	client := func(url string) *jira.Client {
		return jira.New(jira.Config{BaseURL: url, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	}
	other := Options{Client: client(base), Store: st, Site: "work"}
	opt := Options{Client: client(gone.URL), Store: st, Site: "", Sites: []string{"", "work"},
		Open: func(string) (Options, error) { return other, nil }}
	ts := httptest.NewServer(New(context.Background(), opt))
	t.Cleanup(ts.Close)
	var out struct{ Issues map[string]AgentIssue }
	if c := workCall(t, "GET", ts.URL+"/api/agents/issues?keys=demo-5,DEMO-9999,bad%20key", "", &out); c != 200 {
		t.Fatalf("issues = %d", c)
	}
	if is := out.Issues["DEMO-5"]; !is.Found || is.Site != "work" || is.Card.Summary == "" || !strings.HasSuffix(is.URL, "/browse/DEMO-5") {
		t.Errorf("DEMO-5 = %+v", is)
	}
	if is, ok := out.Issues["DEMO-9999"]; !ok || is.Found {
		t.Errorf("DEMO-9999 = %+v %v", is, ok)
	}
	if len(out.Issues) != 2 {
		t.Errorf("issues = %v", out.Issues)
	}
}

// A done issue's worktree goes only when it is clean and merged; the branch stays.
func TestRemoveWorktree(t *testing.T) {
	f, c := newFakeHerdr(t)
	f.open = true
	old := herdrClient
	herdrClient = func() *herdr.Client { return c }
	defer func() { herdrClient = old }()
	repo := gitRepo(t)
	wt := filepath.Join(t.TempDir(), "demo-12")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "issue/DEMO-12-drop", wt).CombinedOutput(); err != nil {
		t.Skip("git worktree:", err, string(out))
	}
	ts := agentsServer(t, repo)
	if c := workCall(t, "DELETE", ts.URL+"/api/issues/DEMO-5/worktree", "", nil); c != 400 {
		t.Errorf("not done = %d", c)
	}
	if err := os.WriteFile(filepath.Join(wt, "wip"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var e map[string]string
	if c := workCall(t, "DELETE", ts.URL+"/api/issues/DEMO-12/worktree", "", &e); c != 400 || !strings.Contains(e["error"], "uncommitted") || f.called("worktree.remove") {
		t.Errorf("dirty = %d %v", c, e)
	}
	_ = os.Remove(filepath.Join(wt, "wip"))
	var out map[string]string
	if c := workCall(t, "DELETE", ts.URL+"/api/issues/DEMO-12/worktree", "", &out); c != 200 || out["Branch"] != "issue/DEMO-12-drop" || !f.called("worktree.remove") {
		t.Errorf("clean = %d %v", c, out)
	}
}
