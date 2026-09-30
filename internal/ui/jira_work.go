package ui

import (
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
)

// Start work (S in the Jira panel): open the issue's worktree as a herdr
// workspace — reusing a local branch that fits ui.work_branch_template when
// one exists, else creating one from it (issue/KEY-slug by default), by herdr
// or the ui.work_create command — label
// its tab with the key and start the agent (ui.work_agent, Claude by
// default) in it with ui.work_args and the start prompt (jira.start_prompt;
// none starts it without one). S asks in one form: the agent (the kinds
// found on PATH, ui.work_agent first), the branch and the prompt, all
// filled in, plus an Also row ticking ui.start_assigns, ui.start_status
// and ui.timer_on_start when one is set. An issue whose agent already runs gets attached to instead
// (agents.go).

const defaultWorkBranch = "issue/{key}-{summary}"

const defaultJiraStartPrompt = "Start work on Jira ticket {key}. You are in its worktree, on its branch. " +
	"Fetch the ticket and all its comments with the Atlassian MCP. Then judge it. " +
	"Small and fully clear: take it A-Z — implement, validate (run/update tests where the project has them), " +
	"commit, push, open a merge request, and move the ticket to Code review without commenting in Jira. " +
	"Anything else: don't write code yet. Summarise the ticket, list the questions and decisions that need me, " +
	"propose a plan, and wait."

type jiraWorkMsg struct {
	key     string
	path    string
	pane    string
	agent   string
	running bool // the agent was already running in the worktree
	// skipActions is the form's actions row unticked: no assign, move or
	// timer (startWrites).
	skipActions bool
	err         error
}

// agentKinds are the agent kinds herdr can start.
var agentKinds = []string{"claude", "codex", "gemini", "opencode", "cursor", "copilot", "amp", "pi", "devin", "agy", "cline",
	"omp", "mastracode", "kimi", "kiro", "droid", "grok", "hermes", "kilo", "qodercli", "qwen", "letta", "maki", "muse"}

// startJiraWork starts work on the panel's issue: attaches to its running
// agent, else asks which agent to start.
func (m *Model) startJiraWork() tea.Cmd {
	iss := m.jiraIssue
	if m.herdr == nil {
		m.status = "start work needs herdr: " + m.noHerdr()
		return nil
	}
	if as := m.agents[iss.Key]; len(as) > 0 {
		return m.attachAgent(iss.Key, as[0].PaneID)
	}
	project, _, _ := strings.Cut(iss.Key, "-")
	if m.jiraRepos[project] == "" {
		m.status = "no jira.repos entry for " + project
		return nil
	}
	if m.jiraStarting[iss.Key] {
		return nil
	}
	m.openWorkForm(iss)
	return nil
}

// The start work form's rows: which agent, the branch and the prompt.
const (
	workAgentField   = "_agent"
	workBranchField  = "_branch"
	workPromptField  = "_prompt"
	workActionsField = "_actions"
)

// openWorkForm asks, in one form, which agent starts on iss (ui.work_agent
// first, then the kinds on PATH), on which branch (the issue's own when the
// repo has one, else the template's) and with which prompt (the start
// prompt, editable; empty starts without one). The cursor waits on the
// button: enter starts with them as they are.
func (m *Model) openWorkForm(iss *jira.Issue) {
	kinds := []jira.Option{{ID: m.opts.workAgent, Name: m.opts.workAgent}}
	for _, k := range agentKinds {
		if _, err := exec.LookPath(k); err == nil && k != m.opts.workAgent {
			kinds = append(kinds, jira.Option{ID: k, Name: k})
		}
	}
	project, _, _ := strings.Cut(iss.Key, "-")
	branch := issueBranch(expandUserPath(m.jiraRepos[project]), m.opts.workBranch, iss.Key, iss.Type)
	if branch == "" {
		branch = branchName(m.opts.workBranch, iss.Key, iss.Type, iss.Summary)
	}
	prompt := m.jiraStartPrompt
	if !strings.EqualFold(strings.TrimSpace(prompt), "none") {
		prompt = strings.ReplaceAll(prompt, "{key}", iss.Key)
	} else {
		prompt = ""
	}
	f := &jiraFormState{key: iss.Key, origin: jiraFromPanel, work: true, fields: []jiraFormField{
		{FieldMeta: jira.FieldMeta{ID: workAgentField, Name: "Agent", Kind: jira.KindOption, Options: kinds}, val: jira.Value{Options: kinds[:1]}},
		{FieldMeta: jira.FieldMeta{ID: workBranchField, Name: "Branch", Kind: jira.KindText}, val: jira.Value{Text: branch}},
		{FieldMeta: jira.FieldMeta{ID: workPromptField, Name: "Prompt", Kind: jira.KindDoc}, val: jira.Value{Text: prompt}},
	}}
	if acts := m.startActions(iss.Key); len(acts) > 0 {
		f.fields = append(f.fields, jiraFormField{FieldMeta: jira.FieldMeta{ID: workActionsField, Name: "Also"},
			val: jira.Value{Text: strings.Join(acts, " · ")}})
	}
	f.idx = len(f.fields)
	m.jiraForm = f
}

// startStatus is the status start work moves key to: its project's
// jira.start_statuses, else ui.start_status; "" for none.
func (m *Model) startStatus(key string) string {
	project, _, _ := strings.Cut(key, "-")
	if s, ok := m.jiraStartStatus[project]; ok {
		return strings.TrimSpace(s)
	}
	return m.opts.startStatus
}

// startActions are what start work on key does besides the agent, in
// words, by ui.start_assigns, startStatus and ui.timer_on_start.
func (m *Model) startActions(key string) []string {
	var acts []string
	if m.opts.startAssigns {
		acts = append(acts, "assign to you")
	}
	if s := m.startStatus(key); s != "" {
		acts = append(acts, "move to "+s)
	}
	if m.opts.timerOnStart {
		acts = append(acts, "start timer")
	}
	return acts
}

// toggleWorkActions ticks or unticks the start work form's actions row: an
// empty value is unticked.
func (m *Model) toggleWorkActions(ff *jiraFormField) {
	if ff.val.Empty() {
		ff.val.Text = strings.Join(m.startActions(m.jiraForm.key), " · ")
	} else {
		ff.val = jira.Value{}
	}
	ff.changed = true
}

// submitWorkForm starts the form's agent on its issue, on its branch, with
// its prompt.
func (m *Model) submitWorkForm() tea.Cmd {
	f := m.jiraForm
	val := func(id string) jira.Value {
		i := slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == id })
		return f.fields[i].val
	}
	kind, branch, prompt := "", strings.TrimSpace(val(workBranchField).Text), strings.TrimSpace(val(workPromptField).Text)
	if o := val(workAgentField).Options; len(o) > 0 {
		kind = o[0].ID
	}
	if kind == "" {
		f.err = "pick an agent"
		return nil
	}
	skip := false
	if i := slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == workActionsField }); i >= 0 {
		skip = f.fields[i].val.Empty()
	}
	key, iss := f.key, m.jiraIssue
	m.jiraForm = nil
	if iss == nil || iss.Key != key || m.herdr == nil || m.jiraStarting[key] {
		return nil
	}
	if prompt == "" {
		prompt = "none"
	}
	if m.jiraStarting == nil {
		m.jiraStarting = map[string]bool{}
	}
	m.jiraStarting[key] = true
	m.status = key + ": starting work…"
	project, _, _ := strings.Cut(key, "-")
	work := jiraWork(m.herdr, expandUserPath(m.jiraRepos[project]), m.opts.workBranch, branch, kind, key, iss.Type, iss.Summary,
		workArgs(m.opts.workArgs, prompt, key), m.opts.workCreate)
	return func() tea.Msg {
		msg := work().(jiraWorkMsg)
		msg.skipActions = skip
		return msg
	}
}

// workArgs are the agent's arguments: extra with {key} replaced, then the
// start prompt, left out when it is "none".
func workArgs(extra []string, prompt, key string) []string {
	var out []string
	for _, a := range extra {
		out = append(out, strings.ReplaceAll(a, "{key}", key))
	}
	if !strings.EqualFold(strings.TrimSpace(prompt), "none") {
		out = append(out, strings.ReplaceAll(prompt, "{key}", key))
	}
	return out
}

// jiraWork opens the issue's worktree on branch (the issue's own, else
// tmpl's, when "") and starts agent there.
func jiraWork(c *herdr.Client, repo, tmpl, branch, agent, key, typ, summary string, args, create []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if branch == "" {
			branch = issueBranch(repo, tmpl, key, typ)
		}
		if branch == "" {
			branch = branchName(tmpl, key, typ, summary)
		}
		wt := worktreeIn(c, repo, branch, defaultBase(repo), key, create)
		path, pane, running, err := agentInWorktree(ctx, c, wt, key, agent, jiraAgentName(key, time.Now()), args)
		return jiraWorkMsg{key: key, path: path, pane: pane, agent: agent, running: running, err: err}
	}
}

// worktreeIn opens repo's worktree on branch as a herdr workspace, making
// it from base when there is none: by herdr, or by running create in repo
// ({branch}, {base} and {key} replaced) and opening what it made.
func worktreeIn(c *herdr.Client, repo, branch, base, key string, create []string) func(context.Context) (herdr.Worktree, error) {
	return func(ctx context.Context) (herdr.Worktree, error) {
		wt, err := c.OpenWorktree(ctx, repo, branch)
		if !herdr.IsCode(err, "worktree_not_found") {
			return wt, err
		}
		if len(create) == 0 {
			return c.CreateWorktree(ctx, repo, branch, base)
		}
		r := strings.NewReplacer("{branch}", branch, "{base}", cmp.Or(base, "HEAD"), "{key}", key)
		argv := make([]string, len(create))
		for i, a := range create {
			argv[i] = r.Replace(a)
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return wt, fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
		}
		return c.OpenWorktree(ctx, repo, branch)
	}
}

// agentInWorktree opens a worktree with open, labels its tab and starts the
// agent kind in it with args. pane is the agent's; running reports one was
// already there.
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

func (m Model) handleJiraWork(msg jiraWorkMsg) (tea.Model, tea.Cmd) {
	delete(m.jiraStarting, msg.key)
	switch {
	case msg.err != nil:
		m.fail(msg.key + ": start work: " + msg.err.Error())
		return m, nil
	case msg.running:
		return m, m.attachAgent(msg.key, msg.pane)
	default:
		m.status = msg.key + ": " + msg.agent + " started in " + msg.path
	}
	if msg.skipActions {
		return m, nil
	}
	writes := m.startWrites(msg.key)
	if m.opts.timerOnStart && m.timer.key == "" {
		status := m.status
		cmd := m.toggleTimer(msg.key)
		m.status = status + " · timer started"
		return m, tea.Batch(cmd, writes)
	}
	return m, writes
}

// startAgent starts the agent kind in pane with args. A fresh pane's shell
// isn't at its prompt yet, and herdr won't start an agent in a busy pane, so
// it retries.
func startAgent(ctx context.Context, c *herdr.Client, kind, name, pane string, args []string) error {
	for try := 0; ; try++ {
		err := c.StartAgent(ctx, name, kind, pane, args)
		if err == nil || try == 9 || ctx.Err() != nil {
			return err
		}
		time.Sleep(time.Second)
	}
}

// jiraAgentName names the herdr agent: herdr wants [a-z][a-z0-9_-]{0,31}, and
// keeps a name bound for a while after its agent quits, so each gets a stamp.
func jiraAgentName(key string, now time.Time) string {
	name := "jira-" + strings.ToLower(key) + "-" + strconv.FormatInt(now.Unix(), 36)
	return name[:min(len(name), 32)]
}

// issueBranch is the repo's local branch that fits tmpl for the issue, any
// summary or none ("issue/ABC-1-old-title", "issue/ABC-1"), "" when there
// is none.
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

// branchPattern matches the branch names tmpl gives the issue, whatever its
// summary was then, or without one. Literal text stays literal, so
// issue/ABC-1-* is not ABC-12's.
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
			// "-{summary}" may be missing altogether.
			re.WriteString(regexp.QuoteMeta(lit[:len(lit)-1]) + `(?:-[^/]*)?`)
		} else {
			re.WriteString(regexp.QuoteMeta(lit) + value[p])
		}
		at = loc[1]
	}
	re.WriteString(regexp.QuoteMeta(tmpl[at:]))
	return regexp.MustCompile("^" + re.String() + "$")
}

// defaultBase is the remote's default branch (origin/main), so a new branch
// doesn't start from whatever the main checkout has out.
func defaultBase(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "origin/HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// slugify makes a branch-name slug of a summary: lowercase ASCII words joined
// by dashes, cut at a word boundary to about 40 characters.
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

// startWritesMsg is what start work changed on the issue: did, in words,
// or why it stopped.
type startWritesMsg struct {
	key string
	did []string
	err error
}

// startWrites assigns the started issue to you (ui.start_assigns) and
// moves it to its startStatus, when it isn't so already. A move with a
// screen of its own is left to the move form. nil with neither set.
func (m *Model) startWrites(key string) tea.Cmd {
	assign, status := m.opts.startAssigns, m.startStatus(key)
	if !assign && status == "" {
		return nil
	}
	cur := ""
	if m.jiraIssue != nil && m.jiraIssue.Key == key {
		cur = m.jiraIssue.Status
	}
	c, ctx, moveKey := m.jiraClient, m.ctx, helpKey(m.keys.JiraStatus)
	return func() tea.Msg {
		var did []string
		if assign {
			me, err := c.Myself(ctx)
			if err == nil {
				err = c.SetAssignee(ctx, key, me.AccountID)
			}
			if err != nil {
				return startWritesMsg{key: key, did: did, err: err}
			}
			did = append(did, "assigned to you")
		}
		if status != "" && !strings.EqualFold(cur, status) {
			ts, err := c.TransitionsMeta(ctx, key)
			if err != nil {
				return startWritesMsg{key: key, did: did, err: err}
			}
			i := slices.IndexFunc(ts, func(t jira.TransitionMeta) bool { return strings.EqualFold(t.ToName, status) })
			switch {
			case i < 0:
				return startWritesMsg{key: key, did: did, err: fmt.Errorf("no move to %s from here", status)}
			case ts[i].HasScreen:
				return startWritesMsg{key: key, did: did, err: fmt.Errorf("the move to %s asks for fields: %s moves it", status, moveKey)}
			}
			if err := c.DoTransition(ctx, key, ts[i].ID); err != nil {
				return startWritesMsg{key: key, did: did, err: err}
			}
			did = append(did, "moved to "+ts[i].ToName)
		}
		return startWritesMsg{key: key, did: did}
	}
}

// handleStartWrites says what start work changed and reloads the issue.
func (m Model) handleStartWrites(msg startWritesMsg) (tea.Model, tea.Cmd) {
	if len(msg.did) > 0 {
		m.status = msg.key + " " + strings.Join(msg.did, ", ")
	}
	if msg.err != nil {
		m.fail(msg.key + ": " + msg.err.Error())
	}
	board := m.refreshJiraAfterEdit()
	if r := m.currentRef(); r != nil && r.jiraKey == msg.key {
		return m, tea.Batch(m.loadCurrentRef(), board)
	}
	return m, board
}
