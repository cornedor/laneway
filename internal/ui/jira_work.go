package ui

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/herdr"
)

// Start work (S in the Jira panel): open the issue's worktree as a herdr
// workspace — reusing a local branch that fits ui.work_branch_template when
// one exists, else creating one from it (issue/KEY-slug by default) — label
// its tab with the key and start the agent (ui.work_agent, Claude by
// default) in it on the start prompt.

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
	err     error
}

// startJiraWork starts work on the panel's issue.
func (m *Model) startJiraWork() tea.Cmd {
	iss := m.jiraIssue
	c := m.herdr
	if c == nil {
		m.status = "start work needs herdr running"
		return nil
	}
	project, _, _ := strings.Cut(iss.Key, "-")
	repo := m.jiraRepos[project]
	if repo == "" {
		m.status = "no jira.repos entry for " + project
		return nil
	}
	if m.jiraStarting[iss.Key] {
		return nil
	}
	if m.jiraStarting == nil {
		m.jiraStarting = map[string]bool{}
	}
	m.jiraStarting[iss.Key] = true
	m.status = iss.Key + ": starting work…"
	prompt := strings.ReplaceAll(m.jiraStartPrompt, "{key}", iss.Key)
	return jiraWork(c, expandUserPath(repo), m.opts.workBranch, m.opts.workAgent, iss.Key, iss.Type, iss.Summary, prompt)
}

func jiraWork(c *herdr.Client, repo, tmpl, agent, key, typ, summary, prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		branch := issueBranch(repo, tmpl, key, typ)
		if branch == "" {
			branch = branchName(tmpl, key, typ, summary)
		}
		path, pane, running, err := agentInWorktree(ctx, c, repo, branch, defaultBase(repo), key, agent, jiraAgentName(key, time.Now()), prompt)
		return jiraWorkMsg{key: key, path: path, pane: pane, agent: agent, running: running, err: err}
	}
}

// agentInWorktree opens repo's worktree on branch as a herdr workspace
// (creating it from base when there is none), labels its tab and starts
// the agent kind in it on prompt. pane is the agent's; running reports one
// was already there.
func agentInWorktree(ctx context.Context, c *herdr.Client, repo, branch, base, tab, kind, name, prompt string) (path, pane string, running bool, err error) {
	wt, err := c.OpenWorktree(ctx, repo, branch)
	if herdr.IsCode(err, "worktree_not_found") {
		wt, err = c.CreateWorktree(ctx, repo, branch, base)
	}
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
	err = startAgent(ctx, c, kind, name, wt.Pane, []string{prompt})
	return wt.Path, wt.Pane, false, err
}

func (m Model) handleJiraWork(msg jiraWorkMsg) (tea.Model, tea.Cmd) {
	delete(m.jiraStarting, msg.key)
	switch {
	case msg.err != nil:
		m.status = msg.key + ": start work: " + msg.err.Error()
		return m, nil
	case msg.running:
		m.status = msg.key + ": already running in " + msg.path
	default:
		m.status = msg.key + ": " + msg.agent + " started in " + msg.path
	}
	if m.opts.timerOnStart && m.timer.key == "" {
		status := m.status
		cmd := m.toggleTimer(msg.key)
		m.status = status + " · timer started"
		return m, cmd
	}
	return m, nil
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
