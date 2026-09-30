package web

import (
	"cmp"
	"context"
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
	"time"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
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

func repoFor(s *Server, key string) string {
	project, _, _ := strings.Cut(key, "-")
	if r := s.opt.Jira.Repos[project]; r != "" {
		return expandUserPath(r)
	}
	return ""
}

func init() {
	get("/issues/{key}/work", workForm)
	post("/issues/{key}/work", startWork)
	get("/issues/{key}/branch", issueBranchName)
	post("/issues/{key}/pr", openPullRequest)
}

// workForm is what the start work form starts from.
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
	repo := repoFor(s, key)
	kinds := []string{cfg.Agent}
	for _, k := range agentKinds {
		if k != cfg.Agent && have(k) {
			kinds = append(kinds, k)
		}
	}
	branch := ""
	if repo != "" {
		branch = issueBranch(repo, cfg.Branch, key, iss.Type)
	}
	if branch == "" {
		branch = branchName(cfg.Branch, key, iss.Type, iss.Summary)
	}
	prompt := cfg.Prompt
	if strings.EqualFold(strings.TrimSpace(prompt), "none") {
		prompt = ""
	}
	var acts []string
	if cfg.Assigns {
		acts = append(acts, "assign to you")
	}
	if st := startStatus(s, key); st != "" {
		acts = append(acts, "move to "+st)
	}
	if cfg.Timer {
		acts = append(acts, "start timer")
	}
	running := []AgentOut{}
	if c := herdrClient(); c != nil {
		if as, err := c.Agents(ctx); err == nil {
			for _, a := range toOut(as) {
				if a.Key == key {
					running = append(running, a)
				}
			}
		}
	}
	return map[string]any{
		"Herdr": herdrClient() != nil, "Repo": repo, "Agents": kinds, "Agent": cfg.Agent, "Branch": branch,
		"Prompt": strings.ReplaceAll(prompt, "{key}", key), "Actions": acts, "Running": running,
	}, nil
}

var (
	startingMu sync.Mutex
	starting   = map[string]bool{}
)

// startWork opens the worktree and starts the agent, then the configured
// writes; those failing is reported, not fatal.
func startWork(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	b, err := Body[struct {
		Agent, Branch, Prompt string
		Actions               bool
	}](r)
	if err != nil {
		return nil, err
	}
	c, err := needHerdr()
	if err != nil {
		return nil, err
	}
	repo := repoFor(s, key)
	if repo == "" {
		project, _, _ := strings.Cut(key, "-")
		return nil, badRequest("no jira.repos entry for " + project)
	}
	startingMu.Lock()
	if starting[key] {
		startingMu.Unlock()
		return nil, httpError{http.StatusConflict, key + " is starting already"}
	}
	starting[key] = true
	startingMu.Unlock()
	defer func() { startingMu.Lock(); delete(starting, key); startingMu.Unlock() }()

	iss, err := s.Client().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	cfg := workConfigOf(s)
	kind := cmp.Or(strings.TrimSpace(b.Agent), cfg.Agent)
	if b.Agent != "" && !slices.Contains(agentKinds, kind) {
		return nil, badRequest("unknown agent " + kind)
	}
	branch := strings.TrimSpace(b.Branch)
	if branch != "" && !validBranch(ctx, branch) {
		return nil, badRequest("bad branch name " + branch)
	}
	if branch == "" {
		branch = cmp.Or(issueBranch(repo, cfg.Branch, key, iss.Type), branchName(cfg.Branch, key, iss.Type, iss.Summary))
	}
	prompt := strings.TrimSpace(b.Prompt)
	if prompt == "" {
		prompt = "none"
	}
	open := worktreeIn(c, repo, branch, defaultBase(repo), key, cfg.Create)
	path, pane, running, err := agentInWorktree(ctx, c, open, key, kind, agentName(key, time.Now()), workArgs(cfg.Args, prompt, key))
	forgetWorktrees()
	if err != nil {
		return nil, fmt.Errorf("start work: %w", err)
	}
	out := map[string]any{"Path": path, "Pane": pane, "Agent": kind, "Running": running, "Branch": branch, "Did": []string{}}
	if running || !b.Actions {
		return out, nil
	}
	did, werr := startWrites(ctx, s, key, iss.Status, cfg.Assigns)
	out["Did"] = did
	if werr != nil {
		out["Warn"] = werr.Error()
	}
	out["Timer"] = cfg.Timer
	s.Client().Invalidate(key)
	return out, nil
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
func startWrites(ctx context.Context, s *Server, key, current string, assign bool) ([]string, error) {
	did := []string{}
	c := s.Client()
	if assign {
		me, err := c.Myself(ctx)
		if err == nil {
			err = c.SetAssignee(ctx, key, me.AccountID)
		}
		if err != nil {
			return did, err
		}
		did = append(did, "assigned to you")
	}
	if st := startStatus(s, key); st != "" && !strings.EqualFold(current, st) {
		ts, err := c.TransitionsMeta(ctx, key)
		if err != nil {
			return did, err
		}
		i := slices.IndexFunc(ts, func(t jira.TransitionMeta) bool { return strings.EqualFold(t.ToName, st) })
		switch {
		case i < 0:
			return did, fmt.Errorf("no move to %s from here", st)
		case ts[i].HasScreen:
			return did, fmt.Errorf("the move to %s asks for fields: move it with s", st)
		}
		if err := c.DoTransition(ctx, key, ts[i].ID); err != nil {
			return did, err
		}
		did = append(did, "moved to "+ts[i].ToName)
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
		return "", fmt.Errorf("%s: %s", name, cliError(err))
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
		return nil, badRequest("no jira.repos entry for " + strings.SplitN(key, "-", 2)[0])
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

// worktreeIn opens repo's worktree on branch as a herdr workspace, making it
// from base when there is none: by herdr, or by running create in repo.
func worktreeIn(c *herdr.Client, repo, branch, base, key string, create []string) func(context.Context) (herdr.Worktree, error) {
	return func(ctx context.Context) (herdr.Worktree, error) {
		wt, err := c.OpenWorktree(ctx, repo, branch)
		if !herdr.IsCode(err, "worktree_not_found") {
			return wt, err
		}
		if len(create) == 0 {
			return c.CreateWorktree(ctx, repo, branch, base)
		}
		rp := strings.NewReplacer("{branch}", branch, "{base}", cmp.Or(base, "HEAD"), "{key}", key)
		argv := make([]string, len(create))
		for i, a := range create {
			argv[i] = rp.Replace(a)
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return wt, fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
		}
		return c.OpenWorktree(ctx, repo, branch)
	}
}

// agentInWorktree opens a worktree, labels its tab and starts the agent kind
// in it. running reports an agent was already there.
func agentInWorktree(ctx context.Context, c *herdr.Client, open func(context.Context) (herdr.Worktree, error), tab, kind, name string, args []string) (path, pane string, running bool, err error) {
	wt, err := open(ctx)
	if err != nil {
		return "", "", false, err
	}
	if wt.AlreadyOpen {
		if agents, err := c.Agents(ctx); err == nil {
			for _, a := range agents {
				if a.WorkspaceID == wt.Workspace && a.Agent != "" {
					return wt.Path, a.PaneID, true, nil
				}
			}
		}
	}
	_ = c.RenameTab(ctx, wt.Tab, tab)
	err = startAgent(ctx, c, kind, name, wt.Pane, args)
	return wt.Path, wt.Pane, false, err
}

// startAgent retries: a fresh pane's shell isn't at its prompt yet, and herdr
// won't start an agent in a busy pane.
var startAgentWait = time.Second

func startAgent(ctx context.Context, c *herdr.Client, kind, name, pane string, args []string) error {
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
	}
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
