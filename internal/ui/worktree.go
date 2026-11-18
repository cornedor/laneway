package ui

import (
	"cmp"
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/vcs"
)

// A → Remove its worktree, on a done issue: once its branch is merged, the
// checkout goes (herdr's worktree.remove, the branch stays). Uncommitted
// changes, or a branch not merged yet, keep it.

// linkedWorktrees are repo's other checkouts on a branch (not its main
// one): git's linked worktrees, jj's workspaces. path → branch.
func linkedWorktrees(repo string) map[string]string {
	return vcs.Checkouts(repo)
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
	switch err := vcs.Kept(repo, path, branch, base); {
	case err == nil:
		return ""
	case errors.Is(err, vcs.ErrUncommitted):
		return i18n.T("it has uncommitted changes")
	case errors.Is(err, vcs.ErrUnmerged):
		return i18n.Tf("%s is not merged into %s", branch, cmp.Or(base, vcs.DefaultMerged(repo)))
	default:
		return err.Error()
	}
}

// DropWorktree removes the checkout at path on branch: through the
// multiplexer, or in a jj repo it doesn't do workspaces of, by forgetting
// the workspace and closing the one open on it.
func DropWorktree(ctx context.Context, c *herdr.Client, repo, path, branch string) error {
	if !vcs.IsJJ(repo) || c.JJWorkspaces {
		wt, err := c.OpenWorktree(ctx, repo, branch)
		if err != nil {
			return err
		}
		return c.RemoveWorktree(ctx, wt.Workspace)
	}
	if err := vcs.ForgetWorkspace(ctx, repo, path); err != nil {
		return err
	}
	if wss, err := c.Workspaces(ctx); err == nil {
		for _, ws := range wss {
			if ws.Label == branch {
				return c.CloseWorkspace(ctx, ws.ID)
			}
		}
	}
	return nil
}

// removeWorktree removes the done issue's worktree when nothing keeps it.
func (m *Model) removeWorktree(key string) tea.Cmd {
	iss, c := m.jiraIssue, m.herdr
	repo := expandUserPath(m.jiraRepos[issueProject(key)])
	switch {
	case iss == nil || iss.Key != key || repo == "":
		return nil
	case c == nil:
		m.status = i18n.T("removing a worktree needs herdr running")
		return nil
	}
	tmpl, typ := m.opts.workBranch, iss.Type
	m.status = i18n.Tf("%s: removing its worktree…", key)
	return agentCall(key, i18n.T("worktree removed"), func(ctx context.Context) error {
		path, branch := issueWorktree(repo, tmpl, key, typ)
		if path == "" {
			return errors.New(i18n.T("no worktree"))
		}
		if why := worktreeGone(repo, path, branch, defaultBase(repo)); why != "" {
			return errors.New(i18n.Tf("keeping %s: %s", homeShort(path), why))
		}
		return DropWorktree(ctx, c, repo, path, branch)
	})
}
