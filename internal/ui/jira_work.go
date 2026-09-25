package ui

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/herdr"
)

// Start work (S in the Jira panel): open the issue's worktree as a herdr
// workspace — reusing an issue/KEY-* branch when one exists, else creating
// issue/KEY-slug — label its tab with the key and start Claude in it on the
// start prompt.

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
	running bool // Claude was already running in the worktree
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
	return jiraWork(c, expandUserPath(repo), iss.Key, iss.Summary, prompt)
}

func jiraWork(c *herdr.Client, repo, key, summary, prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		branch := issueBranch(repo, key)
		if branch == "" {
			branch = "issue/" + key + "-" + slugify(summary)
		}
		path, pane, running, err := claudeInWorktree(ctx, c, repo, branch, defaultBase(repo), key, jiraAgentName(key, time.Now()), prompt)
		return jiraWorkMsg{key: key, path: path, pane: pane, running: running, err: err}
	}
}

// claudeInWorktree opens repo's worktree on branch as a herdr workspace
// (creating it from base when there is none), labels its tab and starts
// Claude in it on prompt. pane is Claude's; running reports it was already
// there.
func claudeInWorktree(ctx context.Context, c *herdr.Client, repo, branch, base, tab, name, prompt string) (path, pane string, running bool, err error) {
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
	err = startClaude(ctx, c, name, wt.Pane, []string{prompt})
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
		m.status = msg.key + ": claude started in " + msg.path
	}
	if m.opts.timerOnStart && m.timer.key == "" {
		status := m.status
		cmd := m.toggleTimer(msg.key)
		m.status = status + " · timer started"
		return m, cmd
	}
	return m, nil
}

// startClaude starts Claude in pane with args. A fresh pane's shell isn't at
// its prompt yet, and herdr won't start an agent in a busy pane, so it retries.
func startClaude(ctx context.Context, c *herdr.Client, name, pane string, args []string) error {
	for try := 0; ; try++ {
		err := c.StartAgent(ctx, name, "claude", pane, args)
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

// issueBranch is the repo's local issue/KEY-* branch, "" when there is none.
func issueBranch(repo, key string) string {
	out, err := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname:short)",
		"refs/heads/issue/"+key, "refs/heads/issue/"+key+"-*").Output()
	if err != nil {
		return ""
	}
	for _, b := range strings.Fields(string(out)) {
		rest := strings.TrimPrefix(b, "issue/"+key)
		// issue/ABC-1-* is not ABC-12's.
		if rest == "" || rest[0] == '-' && (len(rest) == 1 || rest[1] < '0' || rest[1] > '9') {
			return b
		}
	}
	return ""
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
