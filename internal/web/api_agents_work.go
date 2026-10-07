package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cornedor/laneway/internal/cli"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// Start work on an issue (the TUI's S): its worktree as a herdr workspace, an
// agent in it with the start prompt, the issue moved and assigned as
// configured. Also the issue's branch name and a draft pull request. The
// git helpers mirror internal/ui (jira_work.go, worktree.go, clipboard.go).

const (
	defaultBranchTemplate = "{key}-{summary}"
	defaultWorkBranch     = "issue/{key}-{summary}"
	defaultStartPrompt    = "Start work on Jira ticket {key}. You are in its worktree, on its branch. " +
		"Fetch the ticket and all its comments with the Atlassian MCP. Then judge it. " +
		"Small and fully clear: take it A-Z — implement, validate (run/update tests where the project has them), " +
		"commit, push, open a merge request, and move the ticket to Code review without commenting in Jira. " +
		"Anything else: don't write code yet. Summarise the ticket, list the questions and decisions that need me, " +
		"propose a plan, and wait."
)

// agentKinds are the agent kinds herdr can start.
var agentKinds = []string{"claude", "codex", "gemini", "opencode", "cursor", "copilot", "amp", "pi", "devin", "agy", "cline",
	"omp", "mastracode", "kimi", "kiro", "droid", "grok", "hermes", "kilo", "qodercli", "qwen", "letta", "maki", "muse"}

var branchPlaceholder = regexp.MustCompile(`\{[^}]*\}`)

type workConfig struct {
	Agent        string
	Args, Create []string
	Branch       string // start work's branch template
	CopyBranch   string // the copied branch name's template
	Prompt       string
	Assigns      bool
	Timer        bool
}

func knownTemplate(t string) bool {
	for _, p := range branchPlaceholder.FindAllString(t, -1) {
		switch p {
		case "{key}", "{summary}", "{type}", "{project}":
		default:
			return false
		}
	}
	return true
}

func workConfigOf(s *Server) workConfig {
	u, j := s.UIConfig(), s.opt.Jira
	c := workConfig{Agent: "claude", CopyBranch: defaultBranchTemplate, Branch: defaultWorkBranch, Prompt: defaultStartPrompt}
	if t := strings.TrimSpace(u.BranchTemplate); t != "" && knownTemplate(t) {
		c.CopyBranch, c.Branch = t, t
	}
	if t := strings.TrimSpace(u.WorkBranchTemplate); t != "" && knownTemplate(t) {
		c.Branch = t
	}
	if a := strings.TrimSpace(u.WorkAgent); a != "" {
		c.Agent = a
	}
	c.Args = slices.DeleteFunc(slices.Clone(u.WorkArgs), func(a string) bool { return a == "" })
	if len(u.WorkCreate) > 0 && strings.TrimSpace(u.WorkCreate[0]) != "" {
		c.Create = slices.Clone(u.WorkCreate)
	}
	if j.StartPrompt != "" {
		c.Prompt = j.StartPrompt
	}
	c.Assigns = strings.EqualFold(strings.TrimSpace(u.StartAssigns), "on")
	c.Timer = strings.EqualFold(strings.TrimSpace(u.TimerOnStart), "on")
	return c
}

// startStatus is the status start work moves key to: the project's
// jira.start_statuses, else ui.start_status; "" for none.
func startStatus(s *Server, key string) string {
	project, _, _ := strings.Cut(key, "-")
	if st, ok := s.opt.Jira.StartStatuses[project]; ok {
		return strings.TrimSpace(st)
	}
	return strings.TrimSpace(s.UIConfig().StartStatus)
}

// noRepo is the error for a project without a checkout, with the fix.
func noRepo(project string) string {
	return "no repo for " + project + ": set jira.repos." + project + " to its git checkout in the config (Settings, or config.yaml)"
}

func repoFor(s *Server, key string) string {
	project, _, _ := strings.Cut(key, "-")
	if r := s.opt.Jira.Repos[project]; r != "" {
		return expandUserPath(r)
	}
	return ""
}

func init() {
	get("/issues/{key}/work", workForm)
	handle("POST /api/issues/{key}/work", startWorkRoute)
	get("/issues/{key}/branch", issueBranchName)
	post("/issues/{key}/pr", openPullRequest)
	get("/branch", cwdBranchIssue)
	del("/issues/{key}/worktree", removeWorktree)
}

// removeWorktree removes a done issue's worktree once its branch is merged
// and it has no uncommitted changes (herdr's worktree.remove; the branch
// stays), as the TUI's A menu does.
func removeWorktree(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	repo := repoFor(s, key)
	if repo == "" {
		project, _, _ := strings.Cut(key, "-")
		return nil, badRequest(noRepo(project))
	}
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if iss.StatusCategory != "done" {
		return nil, badRequest(key + " is not done yet")
	}
	path, branch := issueWorktree(repo, workConfigOf(s).Branch, key, iss.Type)
	if path == "" {
		return nil, badRequest(key + " has no worktree")
	}
	if why := worktreeKept(repo, path, branch, defaultBase(repo)); why != "" {
		return nil, badRequest("keeping " + path + ": " + why)
	}
	wt, err := c.OpenWorktree(ctx, repo, branch)
	if err != nil {
		return nil, err
	}
	if err := c.RemoveWorktree(ctx, wt.Workspace); err != nil {
		return nil, err
	}
	forgetWorktrees()
	return map[string]string{"Path": path, "Branch": branch}, nil
}

// issueWorktree is repo's linked worktree whose branch fits tmpl for the
// issue: its path and branch, "" for none.
func issueWorktree(repo, tmpl, key, typ string) (path, branch string) {
	fits := branchPattern(tmpl, key, typ)
	for p, b := range linkedWorktrees(repo) {
		if fits.MatchString(b) {
			return p, b
		}
	}
	return "", ""
}

// worktreeKept is why the worktree at path on branch must stay, "" when it
// can go: it has uncommitted changes, or branch is not in base.
func worktreeKept(repo, path, branch, base string) string {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return "git status: " + cli.Error(err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return "it has uncommitted changes"
	}
	if base == "" {
		base = "HEAD"
	}
	if exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", branch, base).Run() != nil {
		return branch + " is not merged into " + base
	}
	return ""
}

// cwdBranchIssue is the issue of the git branch laneway web started in, as
// the TUI's: first in the palette. It must exist: fix/utf-8 names none.
func cwdBranchIssue(ctx context.Context, s *Server, r *http.Request) (any, error) {
	branch, _ := runIn(ctx, "", "git", "branch", "--show-current")
	key := work.BranchKey(branch)
	if key == "" {
		return map[string]string{}, nil
	}
	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return map[string]string{}, nil
	}
	return map[string]string{"Key": iss.Key, "Summary": iss.Summary, "Branch": branch}, nil
}

// WorkSpot is where an issue's work already happens: a linked worktree of
// its repo or a directory its agents run in, with those agents.
type WorkSpot struct {
	Path, Branch string
	Agents       []AgentOut
}

// WorkForm is everything the start work modal shows, as the TUI's form
// fills it in (jira_work.go openWorkForm).
type WorkForm struct {
	Key, Summary, Type, Status string
	Herdr                      bool
	Repo                       string
	Base                       string   // what a new branch starts from, "" for herdr's default
	Agents                     []string // the kinds to pick from: ui.work_agent, then those on PATH
	Agent                      string   // the default
	Missing                    []string // kinds offered but not on PATH here
	Branch                     string   // the issue's own local branch, else the template's
	BranchExists               bool
	Existing                   []string // local branches naming the issue
	Template                   string
	Prompt                     string // the start prompt with {key} filled in; "" for none
	Actions                    []string
	Running                    []AgentOut
	Worktrees                  []WorkSpot
	Problems                   []string // what stops a start, each with its fix
}

// workSpots are key's worktrees and the directories its agents run in,
// those with agents first.
func workSpots(repo, key string, running []AgentOut) []WorkSpot {
	wts := map[string]string{}
	if repo != "" {
		wts = linkedWorktrees(repo)
	}
	spots := []WorkSpot{}
	at := map[string]int{}
	idx := func(p string) int {
		if i, ok := at[p]; ok {
			return i
		}
		at[p] = len(spots)
		spots = append(spots, WorkSpot{Path: p, Branch: wts[p], Agents: []AgentOut{}})
		return len(spots) - 1
	}
	for _, a := range running {
		if a.CWD != "" {
			i := idx(a.CWD)
			spots[i].Agents = append(spots[i].Agents, a)
		}
	}
	for p, b := range wts {
		if work.BranchKey(b) == key {
			idx(p)
		}
	}
	busy := func(w WorkSpot) int { return min(len(w.Agents), 1) }
	slices.SortStableFunc(spots, func(a, b WorkSpot) int {
		return cmp.Or(busy(b)-busy(a), cmp.Compare(a.Path, b.Path))
	})
	return spots
}

// runningOn are herdr's agents on key.
func runningOn(ctx context.Context, c *herdr.Client, key string) []AgentOut {
	out := []AgentOut{}
	if c == nil {
		return out
	}
	if as, err := c.Agents(ctx); err == nil {
		for _, a := range toOut(as) {
			if a.Key == key {
				out = append(out, a)
			}
		}
	}
	return out
}

func isRepo(repo string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--git-dir").Run() == nil
}

func branchExists(repo, branch string) bool {
	return repo != "" && branch != "" && exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// keyBranches are repo's local branches that name key.
func keyBranches(repo, key string) []string {
	out := []string{}
	b, err := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/").Output()
	if err != nil {
		return out
	}
	for _, br := range strings.Fields(string(b)) {
		if work.BranchKey(br) == key {
			out = append(out, br)
		}
	}
	return out
}

// workForm is the start work modal's form: GET /api/issues/{key}/work.
func workForm(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	cfg := workConfigOf(s)
	project, _, _ := strings.Cut(key, "-")
	c := herdrClient()
	f := WorkForm{Key: key, Summary: iss.Summary, Type: iss.Type, Status: iss.Status, Herdr: c != nil, Repo: repoFor(s, key),
		Agent: cfg.Agent, Agents: []string{cfg.Agent}, Missing: []string{}, Existing: []string{}, Template: cfg.Branch,
		Actions: startActions(s, key, cfg), Problems: []string{}}
	for _, k := range agentKinds {
		if k != cfg.Agent && cli.Have(k) {
			f.Agents = append(f.Agents, k)
		}
	}
	if !cli.Have(cfg.Agent) {
		f.Missing = append(f.Missing, cfg.Agent)
	}
	if !f.Herdr {
		f.Problems = append(f.Problems, "herdr is not running: start herdr (the agents run in it), then open this again")
	}
	switch {
	case f.Repo == "":
		f.Problems = append(f.Problems, noRepo(project))
	case !isRepo(f.Repo):
		f.Problems = append(f.Problems, "jira.repos."+project+" is "+f.Repo+", which is not a git checkout: point it at the repository (Settings, or config.yaml)")
		f.Repo = ""
	}
	if f.Repo != "" {
		f.Branch = issueBranch(f.Repo, cfg.Branch, key, iss.Type)
		f.BranchExists = f.Branch != ""
		f.Base = defaultBase(f.Repo)
		f.Existing = keyBranches(f.Repo, key)
	}
	if f.Branch == "" {
		f.Branch = branchName(cfg.Branch, key, iss.Type, iss.Summary)
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Prompt), "none") {
		f.Prompt = strings.ReplaceAll(cfg.Prompt, "{key}", key)
	}
	f.Running = runningOn(ctx, c, key)
	f.Worktrees = workSpots(f.Repo, key, f.Running)
	return f, nil
}

// startActions are what start work does besides the agent, in words.
func startActions(s *Server, key string, cfg workConfig) []string {
	acts := []string{}
	if cfg.Assigns {
		acts = append(acts, "assign to you")
	}
	if st := startStatus(s, key); st != "" {
		acts = append(acts, "move to "+st)
	}
	if cfg.Timer {
		acts = append(acts, "start timer")
	}
	return acts
}

var (
	startingMu sync.Mutex
	starting   = map[string]bool{}
)

// startStep is one step of a start as it goes: State is run, ok, warn or err.
type startStep struct{ Step, State, Text string }

// startPlan is a checked start request.
type startPlan struct {
	s                       *Server
	c                       *herdr.Client
	cfg                     workConfig
	key, repo, kind, branch string
	args                    []string
	actions, another        bool
	spot                    *WorkSpot // another: the directory to start in
	iss                     *jira.Issue
}

// planStart checks a POST /api/issues/{key}/work body:
// {Agent, Branch, Prompt, Actions, Another, Path}. Path (with Another) is
// one of the form's Worktrees.
func planStart(ctx context.Context, s *Server, r *http.Request) (*startPlan, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	b, err := Body[struct {
		Agent, Branch, Prompt, Path string
		Actions, Another            bool
	}](r)
	if err != nil {
		return nil, err
	}
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	project, _, _ := strings.Cut(key, "-")
	repo := repoFor(s, key)
	if repo == "" {
		return nil, badRequest(noRepo(project))
	}
	cfg := workConfigOf(s)
	kind := cmp.Or(strings.TrimSpace(b.Agent), cfg.Agent)
	if kind != cfg.Agent && !slices.Contains(agentKinds, kind) {
		return nil, badRequest("unknown agent " + kind + ": pick one herdr knows")
	}
	branch := strings.TrimSpace(b.Branch)
	if branch != "" && !validBranch(ctx, branch) {
		return nil, badRequest("bad branch name " + branch + ": git refuses it (no spaces, .., ~^:?*[\\, leading - or trailing .lock)")
	}
	p := &startPlan{s: s, c: c, cfg: cfg, key: key, repo: repo, kind: kind, branch: branch, actions: b.Actions, another: b.Another}
	prompt := strings.TrimSpace(b.Prompt)
	if prompt == "" {
		prompt = "none"
	} else if strings.HasPrefix(prompt, "-") {
		return nil, badRequest("prompt must not start with -")
	}
	p.args = workArgs(cfg.Args, prompt, key)
	if b.Another {
		spots := workSpots(repo, key, runningOn(ctx, c, key))
		if path := strings.TrimSpace(b.Path); path != "" {
			i := slices.IndexFunc(spots, func(w WorkSpot) bool { return w.Path == path })
			if i < 0 {
				return nil, badRequest(path + " is not a worktree of " + key + ": pick one the form lists")
			}
			p.spot = &spots[i]
		} else if len(spots) > 0 && len(spots[0].Agents) > 0 {
			p.spot = &spots[0] // where its first agent runs, as the TUI's "new agent"
		}
		if p.spot != nil && p.spot.Branch != "" && len(p.spot.Agents) == 0 {
			p.branch = p.spot.Branch
		}
	}
	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	p.iss = iss
	if p.branch == "" {
		p.branch = cmp.Or(issueBranch(repo, cfg.Branch, key, iss.Type), branchName(cfg.Branch, key, iss.Type, iss.Summary))
	}
	return p, nil
}

// startWorkRoute runs a start: JSON back, or with Accept:
// application/x-ndjson each step as a line as it happens, then
// {"Done": result} or {"Error": message}. A start goes on when the browser
// leaves: half a start is worse than none.
func startWorkRoute(s *Server, w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	p, err := planStart(ctx, s, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	startingMu.Lock()
	if starting[p.key] {
		startingMu.Unlock()
		writeErr(w, httpError{http.StatusConflict, p.key + " is starting already"})
		return
	}
	starting[p.key] = true
	startingMu.Unlock()
	defer func() { startingMu.Lock(); delete(starting, p.key); startingMu.Unlock() }()

	fl, stream := w.(http.Flusher)
	stream = stream && strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
	if !stream {
		out, err := p.run(ctx, func(startStep) {})
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, r, out)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	enc := json.NewEncoder(w)
	line := func(v any) { _ = enc.Encode(v); fl.Flush() }
	out, err := p.run(ctx, func(st startStep) { line(st) })
	if err != nil {
		line(map[string]string{"Error": err.Error()})
		return
	}
	line(map[string]any{"Done": out})
}

// run opens the worktree and starts the agent, then the configured writes,
// whose failing is reported (Warn), not fatal. As the TUI: an agent running
// in the worktree already is not started twice (Running); another starts
// in a tab of its own.
func (p *startPlan) run(ctx context.Context, report func(startStep)) (map[string]any, error) {
	steps := []startStep{}
	step := func(name, state, text string) {
		st := startStep{name, state, text}
		if n := len(steps); n > 0 && steps[n-1].Step == name {
			steps[n-1] = st
		} else {
			steps = append(steps, st)
		}
		report(st)
	}
	fail := func(name string, err error) (map[string]any, error) {
		err = startError(p.key, p.repo, err)
		step(name, "err", err.Error())
		return nil, err
	}
	c, key := p.c, p.key
	out := map[string]any{"Agent": p.kind, "Branch": p.branch, "Running": false, "Did": []string{}, "Timer": false, "Steps": &steps}
	defer forgetWorktrees()

	// Another agent where one runs: a tab of its own beside it.
	if p.another && p.spot != nil && len(p.spot.Agents) > 0 {
		out["Branch"], out["Path"] = cmp.Or(p.spot.Branch, p.branch), p.spot.Path
		step("tab", "run", "Opening a tab in "+homeShort(p.spot.Path))
		tab, pane, err := c.NewTab(ctx, p.spot.Agents[0].WorkspaceID, key, p.spot.Path, nil)
		if err != nil {
			return fail("tab", err)
		}
		step("tab", "ok", "Opened a tab in "+homeShort(p.spot.Path))
		if err := p.startIn(ctx, pane, step); err != nil {
			_ = c.CloseTab(context.WithoutCancel(ctx), tab)
			return fail("agent", err)
		}
		out["Pane"] = pane
		return out, nil
	}

	step("worktree", "run", "Opening the worktree on "+p.branch)
	wt, how, err := worktreeIn(c, p.repo, p.branch, defaultBase(p.repo), key, p.cfg.Create)(ctx)
	if err != nil {
		return fail("worktree", err)
	}
	out["Path"] = wt.Path
	step("worktree", "ok", homeShort(wt.Path)+" · "+how)
	if wt.AlreadyOpen && !p.another {
		if as, err := c.Agents(ctx); err == nil {
			for _, a := range as {
				if a.WorkspaceID == wt.Workspace && a.Agent != "" {
					step("agent", "ok", "Its agent runs already: "+cmp.Or(a.Name, a.Agent))
					out["Running"], out["Pane"] = true, a.PaneID
					return out, nil
				}
			}
		}
	}
	pane := wt.Pane
	if p.another && wt.AlreadyOpen {
		step("tab", "run", "Opening a tab in "+homeShort(wt.Path))
		tab, np, err := c.NewTab(ctx, wt.Workspace, key, wt.Path, nil)
		if err != nil {
			return fail("tab", err)
		}
		step("tab", "ok", "Opened a tab in "+homeShort(wt.Path))
		if err := p.startIn(ctx, np, step); err != nil {
			_ = c.CloseTab(context.WithoutCancel(ctx), tab)
			return fail("agent", err)
		}
		out["Pane"] = np
		return out, nil
	}
	_ = c.RenameTab(ctx, wt.Tab, key)
	if err := p.startIn(ctx, pane, step); err != nil {
		return fail("agent", err)
	}
	out["Pane"] = pane
	if p.another || !p.actions {
		return out, nil
	}
	did, werr := startWrites(ctx, p.s, key, p.iss.Status, p.cfg.Assigns, step)
	out["Did"] = did
	if werr != nil {
		out["Warn"] = werr.Error()
	}
	out["Timer"] = p.cfg.Timer
	p.s.Client().Invalidate(key)
	return out, nil
}

// startIn starts the plan's agent in pane, saying so while the shell isn't
// ready yet.
func (p *startPlan) startIn(ctx context.Context, pane string, step func(name, state, text string)) error {
	step("agent", "run", "Starting "+p.kind)
	err := startAgentTries(ctx, p.c, p.kind, agentName(p.key, time.Now()), pane, p.args, func(try int) {
		step("agent", "run", fmt.Sprintf("Starting %s: waiting for the shell (try %d of 10)", p.kind, try+1))
	})
	if err != nil {
		return fmt.Errorf("%s did not start: %w: look at its tab in herdr", p.kind, err)
	}
	step("agent", "ok", p.kind+" started")
	return nil
}

// startError says what failed and how to fix what can be fixed.
func startError(key, repo string, err error) error {
	msg := err.Error()
	has := func(s ...string) bool {
		return slices.ContainsFunc(s, func(x string) bool { return strings.Contains(msg, x) })
	}
	switch {
	case herdr.IsCode(err, "worktree_dirty"), has("uncommitted"):
		return fmt.Errorf("%s: the worktree has uncommitted changes: commit or stash them in %s, then start again", key, repo)
	case has("already exists", "already checked out", "is already used by worktree"):
		return fmt.Errorf("%s: the branch is taken (%w): pick another branch name, or its worktree", key, err)
	case has("invalid reference", "not a valid object name", "unknown revision"):
		return fmt.Errorf("%s: git doesn't know the base (%w): git fetch in %s, then start again", key, err, repo)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: herdr took too long: what it opened stays open; start again to go on", key)
	case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENOENT) && has("dial"):
		return fmt.Errorf("%s: herdr stopped answering (%w): is it still running?", key, err)
	}
	return fmt.Errorf("%s: start work: %w", key, err)
}

// validBranch: git's branch name rules, and no leading "-" or "@{" forms.
func validBranch(ctx context.Context, b string) bool {
	if b == "" || strings.HasPrefix(b, "-") || strings.Contains(b, "@{") {
		return false
	}
	return exec.CommandContext(ctx, "git", "check-ref-format", "--branch", b).Run() == nil
}

// startWrites assigns the issue to you and moves it to its start status, when
// it isn't there already.
func startWrites(ctx context.Context, s *Server, key, current string, assign bool, step func(name, state, text string)) ([]string, error) {
	did := []string{}
	c := s.Client()
	if assign {
		step("assign", "run", "Assigning to you")
		me, err := c.Myself(ctx)
		if err == nil {
			err = c.SetAssignee(ctx, key, me.AccountID)
		}
		if err != nil {
			step("assign", "warn", "Not assigned: "+err.Error())
			return did, err
		}
		did = append(did, "assigned to you")
		step("assign", "ok", "Assigned to you")
	}
	if st := startStatus(s, key); st != "" && !strings.EqualFold(current, st) {
		step("move", "run", "Moving to "+st)
		ts, err := c.TransitionsMeta(ctx, key)
		if err == nil {
			i := slices.IndexFunc(ts, func(t jira.TransitionMeta) bool { return strings.EqualFold(t.ToName, st) })
			switch {
			case i < 0:
				err = fmt.Errorf("no move to %s from here", st)
			case ts[i].HasScreen:
				err = fmt.Errorf("the move to %s asks for fields: move it with s", st)
			default:
				if err = c.DoTransition(ctx, key, ts[i].ID); err == nil {
					did = append(did, "moved to "+ts[i].ToName)
					step("move", "ok", "Moved to "+ts[i].ToName)
				}
			}
		}
		if err != nil {
			step("move", "warn", "Not moved: "+err.Error())
			return did, err
		}
	}
	return did, nil
}

func issueBranchName(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return map[string]string{"Name": branchName(workConfigOf(s).CopyBranch, key, iss.Type, iss.Summary)}, nil
}

// runIn runs a command in dir and returns its trimmed output; tests swap it.
var runIn = func(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %s", name, cli.Error(err))
	}
	return strings.TrimSpace(string(out)), nil
}

// openPullRequest pushes the issue's branch and opens a draft pull (gh, for
// a GitHub origin) or merge request (glab, else).
func openPullRequest(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	repo := repoFor(s, key)
	if repo == "" {
		return nil, badRequest(noRepo(strings.SplitN(key, "-", 2)[0]))
	}
	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	branch := issueBranch(repo, workConfigOf(s).Branch, key, iss.Type)
	if branch == "" {
		return nil, badRequest("no branch for " + key + " in " + repo + ": start work creates one")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	remote, err := runIn(ctx, repo, "git", "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}
	if _, err := runIn(ctx, repo, "git", "push", "-u", "origin", branch); err != nil {
		return nil, err
	}
	title, body := key+" "+iss.Summary, "Jira: "+s.Client().BrowseURL(key)
	var out string
	if strings.Contains(remote, "github") {
		out, err = runIn(ctx, repo, "gh", "pr", "create", "--draft", "--head", branch, "--title", title, "--body", body)
	} else {
		out, err = runIn(ctx, repo, "glab", "mr", "create", "--draft", "--source-branch", branch, "--title", title, "--description", body, "--yes")
	}
	if err != nil {
		return nil, err
	}
	url := out
	if i := strings.LastIndex(out, "http"); i >= 0 {
		url = strings.Fields(out[i:])[0]
	}
	return map[string]string{"URL": url, "Branch": branch}, nil
}

// workArgs are the agent's arguments: extra with {key} replaced, then the
// start prompt, left out when it is "none".
func workArgs(extra []string, prompt, key string) []string {
	var out []string
	for _, a := range extra {
		out = append(out, strings.ReplaceAll(a, "{key}", key))
	}
	if !strings.EqualFold(strings.TrimSpace(prompt), "none") && strings.TrimSpace(prompt) != "" {
		out = append(out, strings.ReplaceAll(prompt, "{key}", key))
	}
	return out
}

// agentName names the herdr agent: [a-z][a-z0-9_-]{0,31}, stamped because
// herdr keeps a name bound a while after its agent quits.
func agentName(key string, now time.Time) string {
	name := "jira-" + strings.ToLower(key) + "-" + strconv.FormatInt(now.Unix(), 36)
	return name[:min(len(name), 32)]
}

// safeArg: what may fill a placeholder of a configured command: no leading
// "-", no whitespace, quotes or shell characters.
var safeArg = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/+@-]*$`)

// worktreeIn opens repo's worktree on branch as a herdr workspace, making it
// from base when there is none: by herdr, or by running create in repo. how
// says which, in words.
func worktreeIn(c *herdr.Client, repo, branch, base, key string, create []string) func(context.Context) (wt herdr.Worktree, how string, err error) {
	return func(ctx context.Context) (herdr.Worktree, string, error) {
		wt, err := c.OpenWorktree(ctx, repo, branch)
		switch {
		case err == nil && wt.AlreadyOpen:
			return wt, "already open in herdr", nil
		case err == nil:
			return wt, "its worktree", nil
		case !herdr.IsCode(err, "worktree_not_found"):
			return wt, "", err
		}
		how := "new worktree on the existing branch"
		if !branchExists(repo, branch) {
			how = "new worktree, branch from " + cmp.Or(base, "herdr's default")
		}
		if len(create) == 0 {
			wt, err := c.CreateWorktree(ctx, repo, branch, base)
			return wt, how, err
		}
		base = cmp.Or(base, "HEAD")
		for _, v := range []string{branch, base, key} {
			if !safeArg.MatchString(v) {
				return wt, "", fmt.Errorf("create: %q is not safe to hand to a command", v)
			}
		}
		// The program is the configured one as written; only its arguments
		// take the placeholders, each already matched against safeArg.
		rp := strings.NewReplacer("{branch}", branch, "{base}", base, "{key}", key)
		args := make([]string, len(create)-1)
		for i, a := range create[1:] {
			args[i] = rp.Replace(a)
		}
		argv := append([]string{create[0]}, args...)
		cmd := exec.CommandContext(ctx, create[0], args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return wt, "", fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
		}
		wt, err = c.OpenWorktree(ctx, repo, branch)
		return wt, "made by " + argv[0], err
	}
}

// startAgent retries: a fresh pane's shell isn't at its prompt yet, and herdr
// won't start an agent in a busy pane.
var startAgentWait = time.Second

func startAgent(ctx context.Context, c *herdr.Client, kind, name, pane string, args []string) error {
	return startAgentTries(ctx, c, kind, name, pane, args, nil)
}

// startAgentTries is startAgent telling retry each try after the first.
func startAgentTries(ctx context.Context, c *herdr.Client, kind, name, pane string, args []string, retry func(try int)) error {
	for try := 0; ; try++ {
		err := c.StartAgent(ctx, name, kind, pane, args)
		if err == nil || try == 9 || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(startAgentWait):
		}
		if retry != nil {
			retry(try + 1)
		}
	}
}

// homeShort writes p under the home directory as ~/….
func homeShort(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(p, home); ok && (rest == "" || rest[0] == '/') {
			return "~" + rest
		}
	}
	return p
}

func expandUserPath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// linkedWorktrees are repo's linked worktrees (not its main checkout) that
// have a branch: path → branch.
func linkedWorktrees(repo string) map[string]string {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	wts := map[string]string{}
	for i, block := range strings.Split(strings.TrimSpace(string(out)), "\n\n") {
		if i == 0 {
			continue
		}
		var p, b string
		for _, l := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(l, "worktree "); ok {
				p = v
			}
			if v, ok := strings.CutPrefix(l, "branch refs/heads/"); ok {
				b = v
			}
		}
		if b != "" {
			wts[p] = b
		}
	}
	return wts
}

// issueBranch is the repo's local branch that fits tmpl for the issue, any
// summary or none, "" when there is none.
func issueBranch(repo, tmpl, key, typ string) string {
	out, err := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/").Output()
	if err != nil {
		return ""
	}
	fits := branchPattern(tmpl, key, typ)
	for _, b := range strings.Fields(string(out)) {
		if fits.MatchString(b) {
			return b
		}
	}
	return ""
}

func branchPattern(tmpl, key, typ string) *regexp.Regexp {
	project, _, _ := strings.Cut(key, "-")
	if typ != "" {
		typ = slugify(typ)
	}
	value := map[string]string{
		"{key}": regexp.QuoteMeta(key), "{type}": regexp.QuoteMeta(typ),
		"{project}": regexp.QuoteMeta(project), "{summary}": `[^/]*`,
	}
	var re strings.Builder
	at := 0
	for _, loc := range branchPlaceholder.FindAllStringIndex(tmpl, -1) {
		lit, p := tmpl[at:loc[0]], tmpl[loc[0]:loc[1]]
		if p == "{summary}" && strings.HasSuffix(lit, "-") {
			re.WriteString(regexp.QuoteMeta(lit[:len(lit)-1]) + `(?:-[^/]*)?`)
		} else {
			re.WriteString(regexp.QuoteMeta(lit) + value[p])
		}
		at = loc[1]
	}
	re.WriteString(regexp.QuoteMeta(tmpl[at:]))
	return regexp.MustCompile("^" + re.String() + "$")
}

// defaultBase is the remote's default branch (origin/main).
func defaultBase(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "origin/HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func branchName(tmpl, issueKey, typ, summary string) string {
	project, _, _ := strings.Cut(issueKey, "-")
	if typ != "" {
		typ = slugify(typ)
	}
	return strings.NewReplacer("{key}", issueKey, "{summary}", slugify(summary), "{type}", typ, "{project}", project).Replace(tmpl)
}

// slugify makes a branch-name slug: lowercase ASCII words joined by dashes,
// cut at a word boundary to about 40 characters.
func slugify(s string) string {
	var words []string
	var w strings.Builder
	flush := func() {
		if w.Len() > 0 {
			words = append(words, w.String())
			w.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			w.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	slug := ""
	for _, word := range words {
		next := word
		if slug != "" {
			next = slug + "-" + word
		}
		if len(next) > 40 && slug != "" {
			break
		}
		slug = next
	}
	if slug == "" {
		return "work"
	}
	return slug
}
