package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// startServer is agentsServer with ui as the config's ui section.
func startServer(t *testing.T, repo string, ui config.UIConfig) *httptest.Server {
	t.Helper()
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	st, _ := store.Open(filepath.Join(t.TempDir(), "state.json"))
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	repos := map[string]string{}
	if repo != "" {
		repos["DEMO"] = repo
	}
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, Store: st, Site: "demo", Jira: config.JiraConfig{Repos: repos}, UI: ui}))
	t.Cleanup(ts.Close)
	return ts
}

// withHerdr points the server at a fake herdr whose agent starts need no wait.
func withHerdr(t *testing.T) *fakeHerdr {
	t.Helper()
	f, c := newFakeHerdr(t)
	old, oldWait := herdrClient, startAgentWait
	herdrClient = func() *herdr.Client { return c }
	startAgentWait = time.Millisecond
	t.Cleanup(func() { herdrClient, startAgentWait = old, oldWait })
	return f
}

// streamStart posts a start asking for its steps line by line.
func streamStart(t *testing.T, url, body string) (steps []startStep, done map[string]any, errMsg string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Accept", "application/x-ndjson")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); res.StatusCode != 200 || ct != "application/x-ndjson" {
		t.Fatalf("stream = %d %s", res.StatusCode, ct)
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		var l struct {
			startStep
			Done  map[string]any
			Error string
		}
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		switch {
		case l.Done != nil:
			done = l.Done
		case l.Error != "":
			errMsg = l.Error
		default:
			steps = append(steps, l.startStep)
		}
	}
	return steps, done, errMsg
}

// stepsSay lists the steps as step:state, a repeat once.
func stepsSay(steps []startStep) string {
	var out []string
	for _, s := range steps {
		if st := s.Step + ":" + s.State; len(out) == 0 || out[len(out)-1] != st {
			out = append(out, st)
		}
	}
	return strings.Join(out, ",")
}

// The form: the TUI's defaults, plus where the issue's work already is.
func TestWorkFormSpec(t *testing.T) {
	f := withHerdr(t)
	repo := gitRepo(t)
	wt := filepath.Join(t.TempDir(), "demo-5")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "issue/DEMO-5-old", wt).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	wt, _ = filepath.EvalSymlinks(wt)
	f.mu.Lock()
	f.agents = append(f.agents, map[string]any{"pane_id": "p9", "name": "jira-demo-5-a", "workspace_id": "w9", "tab_id": "t9", "agent": "codex", "agent_status": "blocked", "cwd": "/elsewhere/demo-5"})
	f.mu.Unlock()
	ts := startServer(t, repo, config.UIConfig{StartAssigns: "on", StartStatus: "In Progress", WorkAgent: "codex"})
	var form WorkForm
	if code := workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form); code != 200 {
		t.Fatalf("form = %d", code)
	}
	if !form.Herdr || form.Agent != "codex" || form.Agents[0] != "codex" || slices.Contains(form.Agents[1:], "codex") {
		t.Errorf("agents = %v default %q", form.Agents, form.Agent)
	}
	if form.Branch != "issue/DEMO-5-old" || !form.BranchExists || !slices.Contains(form.Existing, "issue/DEMO-5-old") || form.Template != defaultWorkBranch {
		t.Errorf("branch = %q exists %v existing %v", form.Branch, form.BranchExists, form.Existing)
	}
	if !strings.Contains(form.Prompt, "DEMO-5") || strings.Contains(form.Prompt, "{key}") {
		t.Errorf("prompt = %q", form.Prompt)
	}
	if strings.Join(form.Actions, ",") != "assign to you,move to In Progress" || len(form.Problems) != 0 || form.Summary == "" {
		t.Errorf("actions %v problems %v summary %q", form.Actions, form.Problems, form.Summary)
	}
	if len(form.Running) != 1 || form.Running[0].PaneID != "p9" {
		t.Errorf("running = %+v", form.Running)
	}
	// The agent's directory first, then the linked worktree without one.
	if len(form.Worktrees) != 2 || form.Worktrees[0].Path != "/elsewhere/demo-5" || len(form.Worktrees[0].Agents) != 1 ||
		form.Worktrees[1].Path != wt || form.Worktrees[1].Branch != "issue/DEMO-5-old" || len(form.Worktrees[1].Agents) != 0 {
		t.Errorf("worktrees = %+v", form.Worktrees)
	}

	// Without a checkout, or without herdr, the form says what to fix.
	ts = startServer(t, "", config.UIConfig{})
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form)
	if len(form.Problems) != 1 || !strings.Contains(form.Problems[0], "jira.repos.DEMO") || form.Branch == "" {
		t.Errorf("no repo: problems %v branch %q", form.Problems, form.Branch)
	}
	ts = startServer(t, t.TempDir(), config.UIConfig{})
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form)
	if len(form.Problems) != 1 || !strings.Contains(form.Problems[0], "not a git checkout") {
		t.Errorf("not a repo: problems %v", form.Problems)
	}
	herdrClient = func() *herdr.Client { return nil }
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-5/work", "", &form)
	if form.Herdr || !strings.Contains(strings.Join(form.Problems, " "), "herdr is not running") {
		t.Errorf("no herdr: %v", form.Problems)
	}
}

// A new worktree, streamed: each step as it goes, then the result.
func TestStartStreamsSteps(t *testing.T) {
	f := withHerdr(t)
	f.startFails = 2 // the fresh shell isn't ready twice
	ts := startServer(t, gitRepo(t), config.UIConfig{StartAssigns: "on", StartStatus: "In Progress", TimerOnStart: "on"})
	steps, done, errMsg := streamStart(t, ts.URL+"/api/issues/DEMO-5/work", `{"Agent":"claude","Branch":"issue/DEMO-5-new","Prompt":"line one\nline two {key}","Actions":true}`)
	if errMsg != "" || done == nil {
		t.Fatalf("error %q done %v", errMsg, done)
	}
	say := stepsSay(steps)
	if !strings.HasPrefix(say, "worktree:run,worktree:ok,agent:run,agent:ok,assign:run,assign:ok,move:run,move:ok") {
		t.Errorf("steps = %s", say)
	}
	if !strings.Contains(steps[1].Text, "branch from") || !slices.ContainsFunc(steps, func(s startStep) bool { return strings.Contains(s.Text, "try 3 of 10") }) {
		t.Errorf("texts = %+v", steps)
	}
	if done["Pane"] != "p2" || done["Timer"] != true || done["Branch"] != "issue/DEMO-5-new" {
		t.Errorf("done = %v", done)
	}
	f.mu.Lock()
	args := f.params["agent.start"]["args"]
	create := f.params["worktree.create"]
	f.mu.Unlock()
	// The multi-line prompt goes as one argument, {key} filled in.
	if a, _ := args.([]any); len(a) != 1 || a[0] != "line one\nline two DEMO-5" {
		t.Errorf("args = %#v", args)
	}
	if create["branch"] != "issue/DEMO-5-new" {
		t.Errorf("worktree.create = %v", create)
	}
}

// An existing worktree with its agent: nothing starts, the agent is named.
func TestStartExistingWorktreeFindsAgent(t *testing.T) {
	f := withHerdr(t)
	f.open = true
	f.mu.Lock()
	f.agents = append(f.agents, map[string]any{"pane_id": "p2", "name": "jira-demo-5-x", "workspace_id": "w2", "tab_id": "t2", "agent": "claude", "agent_status": "idle", "cwd": "/w/demo-5"})
	f.mu.Unlock()
	ts := startServer(t, gitRepo(t), config.UIConfig{StartAssigns: "on"})
	steps, done, _ := streamStart(t, ts.URL+"/api/issues/DEMO-5/work", `{"Actions":true}`)
	if done["Running"] != true || done["Pane"] != "p2" || f.called("agent.start") || f.called("worktree.create") {
		t.Errorf("done = %v, calls %v", done, f.calls)
	}
	if stepsSay(steps) != "worktree:run,worktree:ok,agent:ok" || !strings.Contains(steps[1].Text, "already open") {
		t.Errorf("steps = %+v", steps)
	}
}

// Another agent in a directory the form listed; one it didn't is refused.
func TestStartAnotherInPath(t *testing.T) {
	f := withHerdr(t)
	f.mu.Lock()
	f.agents = append(f.agents,
		map[string]any{"pane_id": "p5", "name": "jira-demo-5-a", "workspace_id": "w5", "tab_id": "t5", "agent": "claude", "agent_status": "idle", "cwd": "/w/demo-5"},
		map[string]any{"pane_id": "p6", "name": "jira-demo-5-b", "workspace_id": "w6", "tab_id": "t6", "agent": "claude", "agent_status": "idle", "cwd": "/w/demo-5-b"})
	f.mu.Unlock()
	ts := startServer(t, gitRepo(t), config.UIConfig{StartAssigns: "on"})
	var e map[string]string
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Another":true,"Path":"/etc"}`, &e); code != 400 || !strings.Contains(e["error"], "not a worktree") {
		t.Errorf("foreign path = %d %v", code, e)
	}
	var res map[string]any
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Another":true,"Path":"/w/demo-5-b","Prompt":"","Actions":true}`, &res); code != 200 || res["Pane"] != "p3" || res["Path"] != "/w/demo-5-b" {
		t.Fatalf("another = %d %v", code, res)
	}
	f.mu.Lock()
	tab, start := f.params["tab.create"], f.params["agent.start"]
	f.mu.Unlock()
	if tab["workspace_id"] != "w6" || tab["cwd"] != "/w/demo-5-b" || tab["label"] != "DEMO-5" || start["pane_id"] != "p3" || start["args"] != nil {
		t.Errorf("tab.create %v agent.start %v", tab, start)
	}
	if d, _ := res["Did"].([]any); len(d) != 0 || f.called("user.assign") {
		t.Errorf("another wrote to Jira: %v", res["Did"])
	}
}

// Failures say what failed and how to fix it; a tab opened for nothing closes.
func TestStartErrors(t *testing.T) {
	f := withHerdr(t)
	f.startFails = -1
	f.mu.Lock()
	f.agents = append(f.agents, map[string]any{"pane_id": "p5", "name": "jira-demo-5-a", "workspace_id": "w5", "tab_id": "t5", "agent": "claude", "agent_status": "idle", "cwd": "/w/demo-5"})
	f.mu.Unlock()
	ts := startServer(t, gitRepo(t), config.UIConfig{})
	steps, _, errMsg := streamStart(t, ts.URL+"/api/issues/DEMO-5/work", `{"Another":true}`)
	if !strings.Contains(errMsg, "claude did not start") || stepsSay(steps) != "tab:run,tab:ok,agent:run,agent:err" || !f.called("tab.close") {
		t.Errorf("start fails: %q %s closed %v", errMsg, stepsSay(steps), f.called("tab.close"))
	}

	f.startFails = 0
	f.createErr = "fatal: 'issue/DEMO-5-x' is already checked out at '/r'"
	var e map[string]string
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Branch":"issue/DEMO-5-x"}`, &e); code == 200 || !strings.Contains(e["error"], "pick another branch name") {
		t.Errorf("taken branch = %d %v", code, e)
	}

	// Starting already: the second one is refused, not doubled.
	startingMu.Lock()
	starting["DEMO-5"] = true
	startingMu.Unlock()
	defer func() { startingMu.Lock(); delete(starting, "DEMO-5"); startingMu.Unlock() }()
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{}`, nil); code != http.StatusConflict {
		t.Errorf("twice = %d", code)
	}
}

// ui.work_agent may name a kind outside the list; anything else is refused.
func TestStartConfiguredAgent(t *testing.T) {
	f := withHerdr(t)
	ts := startServer(t, gitRepo(t), config.UIConfig{WorkAgent: "myagent"})
	if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Agent":"myagent"}`, nil); code != 200 {
		t.Errorf("configured agent = %d", code)
	}
	f.mu.Lock()
	kind := f.params["agent.start"]["kind"]
	f.mu.Unlock()
	if kind != "myagent" {
		t.Errorf("kind = %v", kind)
	}
	for _, a := range []string{"rm", "-x", "claude; rm"} {
		if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", `{"Agent":"`+a+`"}`, nil); code != 400 {
			t.Errorf("%s = %d", a, code)
		}
	}
}
