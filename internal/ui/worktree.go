package ui

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A → Remove its worktree, on a done issue: once its branch is merged, the
// checkout goes (herdr's worktree.remove, the branch stays). Uncommitted
// changes, or a branch not merged yet, keep it.

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
			continue // the main checkout
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

// issueWorktree is the repo's linked worktree whose branch fits tmpl for
// the issue: its path and branch, "" for none.
func issueWorktree(repo, tmpl, key, typ string) (path, branch string) {
	fits := branchPattern(tmpl, key, typ)
	for p, b := range linkedWorktrees(repo) {
		if fits.MatchString(b) {
			return p, b
		}
	}
	return "", ""
}

// worktreeGone reports why the worktree at path on branch must stay, ""
// when it can go: it has uncommitted changes, or branch isn't in base.
func worktreeGone(repo, path, branch, base string) string {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return "git status: " + err.Error()
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

// removeWorktree removes the done issue's worktree when nothing keeps it.
func (m *Model) removeWorktree(key string) tea.Cmd {
	iss, c := m.jiraIssue, m.herdr
	repo := expandUserPath(m.jiraRepos[issueProject(key)])
	switch {
	case iss == nil || iss.Key != key || repo == "":
		return nil
	case c == nil:
		m.status = "removing a worktree needs herdr running"
		return nil
	}
	tmpl, typ := m.opts.workBranch, iss.Type
	m.status = key + ": removing its worktree…"
	return agentCall(key, "worktree removed", func(ctx context.Context) error {
		path, branch := issueWorktree(repo, tmpl, key, typ)
		if path == "" {
			return errors.New("no worktree")
		}
		if why := worktreeGone(repo, path, branch, defaultBase(repo)); why != "" {
			return errors.New("keeping " + homeShort(path) + ": " + why)
		}
		wt, err := c.OpenWorktree(ctx, repo, branch)
		if err != nil {
			return err
		}
		return c.RemoveWorktree(ctx, wt.Workspace)
	})
}
