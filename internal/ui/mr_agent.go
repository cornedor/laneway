package ui

import (
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	pathpkg "path"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/herdr"
)

// C on a GitLab merge request (its panel view or its diff): its source
// branch as a herdr worktree, and the work agent in it on a review prompt.
// The agent leaves its findings in your pending review with laneway mr note,
// for you to edit, drop and submit (S in the diff).
//
// Ported from matterbox's internal/ui/mr_review.go.

// mrReviewPrompt is the agent's: {ref}, {url}, {source}, {target} and {note},
// the command that adds a pending note.
const mrReviewPrompt = "Review GitLab merge request {ref} ({url}): {source} into {target}. " +
	"You are in a worktree on its source branch. Read the description and discussions with glab, " +
	"then review git diff origin/{target}...origin/{source} for bugs, risks and missed requirements. " +
	"Leave each finding as a pending review note on its line: {note} {url} FILE:LINE TEXT " +
	"(FILE:-LINE for a removed line; no FILE:LINE for the merge request as a whole). " +
	"Only the reviewer sees them until they submit. Don't post or submit anything else."

// WithGitLabRepos gives the checkouts of GitLab projects by path (gitlab:
// repos:), looked in before jira.repos for an agent review.
func (m Model) WithGitLabRepos(repos map[string]string) Model {
	m.gitlabRepos = repos
	return m
}

type mrReviewMsg struct {
	ref, path, pane string
	running         bool
	err             error
}

// startMRReview starts the agent on mr, of client c's project ref.
func (m *Model) startMRReview(mr *forge.Change, ref forge.Ref) tea.Cmd {
	label := ref.Repo + "!" + strconv.Itoa(ref.Number)
	switch {
	case m.herdr == nil:
		m.status = m.noHerdr()
		return nil
	case mr.State != forge.StateOpen:
		m.status = label + " is " + mr.State
		return nil
	case m.mrReviewing[label]:
		return nil
	}
	if m.mrReviewing == nil {
		m.mrReviewing = map[string]bool{}
	}
	m.mrReviewing[label] = true
	m.status = label + ": starting the review…"
	c, repos, kind, extra, create := m.herdr, m.gitlabRepos, m.opts.workAgent, m.opts.workArgs, m.opts.workCreate
	known := slices.Collect(maps.Values(m.jiraRepos))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res := mrReviewMsg{ref: label}
		res.path, res.pane, res.running, res.err = StartMRReview(ctx, c, repos, known, mr, ref, kind, extra, create)
		return res
	}
}

// StartMRReview starts agent kind (with extra arguments, before the prompt) on a
// review of mr in a worktree of its source branch: in the checkout repos
// names for its project, else among known's by origin. The TUI's C and the
// web's Agent review share it.
func StartMRReview(ctx context.Context, c *herdr.Client, repos map[string]string, known []string, mr *forge.Change, ref forge.Ref, kind string, extra, create []string) (path, pane string, running bool, err error) {
	label := ref.Repo + "!" + strconv.Itoa(ref.Number)
	note := "laneway mr note"
	if exe, err := os.Executable(); err == nil {
		note = exe + " mr note"
	}
	prompt := strings.NewReplacer("{ref}", label, "{url}", mr.WebURL, "{source}", mr.SourceBranch,
		"{target}", mr.TargetBranch, "{note}", note).Replace(mrReviewPrompt)
	repo := expandUserPath(repos[ref.Repo])
	if repo == "" {
		dirs := make([]string, len(known))
		for i, d := range known {
			dirs[i] = expandUserPath(d)
		}
		repo = checkoutOf(ref.Repo, dirs)
	}
	if repo == "" {
		return "", "", false, errors.New("no checkout of " + ref.Repo + ": add one under gitlab: repos:, or to jira.repos")
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", repo, "fetch", "origin", mr.SourceBranch, mr.TargetBranch).CombinedOutput(); err != nil {
		return "", "", false, errors.New("git fetch: " + strings.TrimSpace(string(out)))
	}
	name := "mr-" + strconv.Itoa(ref.Number) + "-" + strconv.FormatInt(time.Now().Unix(), 36)
	wt := worktreeIn(c, repo, mr.SourceBranch, "origin/"+mr.SourceBranch, "", create)
	return agentInWorktree(ctx, c, wt, pathpkg.Base(ref.Repo)+"!"+strconv.Itoa(ref.Number), kind, name, workArgs(extra, prompt, label))
}

func (m Model) handleMRReview(msg mrReviewMsg) (tea.Model, tea.Cmd) {
	delete(m.mrReviewing, msg.ref)
	switch {
	case msg.err != nil:
		m.fail(msg.ref + ": review: " + msg.err.Error())
		return m, nil
	case msg.running:
		m.status = msg.ref + ": already reviewing in " + msg.path
	default:
		m.status = msg.ref + ": " + m.opts.workAgent + " reviewing in " + msg.path + " · its notes wait in your review"
	}
	return m, m.attachAgentIn(msg.ref, msg.pane, false)
}

// checkoutOf is the checkout among dirs whose origin is the GitLab project
// (group/project), "" when none is.
func checkoutOf(project string, dirs []string) string {
	for _, d := range dirs {
		out, err := exec.Command("git", "-C", d, "remote", "get-url", "origin").Output()
		if err != nil {
			continue
		}
		if remoteIsProject(strings.TrimSpace(string(out)), project) {
			return d
		}
	}
	return ""
}

// remoteIsProject matches ssh (git@host:group/project.git) and https
// (https://host/group/project.git) remotes.
func remoteIsProject(url, project string) bool {
	if _, rest, ok := strings.Cut(url, "://"); ok {
		_, url, _ = strings.Cut(rest, "/")
	} else {
		_, url, _ = strings.Cut(url, ":")
	}
	return strings.TrimSuffix(strings.Trim(url, "/"), ".git") == project
}
