package ui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/cli"
)

// A → Open a pull request: in the issue's jira.repos checkout, pushes its
// branch and opens a draft pull request (gh, for a GitHub origin) or merge
// request (glab, else), titled with the key and summary, linking the issue.

// runIn runs a command in dir and returns its output; a variable for tests.
var runIn = func(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New(name + ": " + cli.Error(err))
	}
	return strings.TrimSpace(string(out)), nil
}

type pullRequestMsg struct {
	key, url string
	err      error
}

// canOpenPullRequest is whether the issue's project has a jira.repos
// checkout to open one from.
func (m *Model) canOpenPullRequest(key string) bool {
	return m.jiraRepos[issueProject(key)] != ""
}

// openPullRequest pushes the shown issue's branch and opens the request.
func (m *Model) openPullRequest(key string) tea.Cmd {
	iss := m.jiraIssue
	repo := expandUserPath(m.jiraRepos[issueProject(key)])
	if iss == nil || iss.Key != key || repo == "" {
		m.status = "no jira.repos entry for " + issueProject(key)
		return nil
	}
	branch := issueBranch(repo, m.opts.workBranch, key, iss.Type)
	if branch == "" {
		m.status = "no branch for " + key + " in " + repo + " · " + helpKey(m.keys.JiraStart) + " starts one"
		return nil
	}
	title, body := key+" "+iss.Summary, "Jira: "+m.jiraClient.BrowseURL(key)
	m.status = "pushing " + branch + " and opening a draft…"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		remote, err := runIn(ctx, repo, "git", "remote", "get-url", "origin")
		if err != nil {
			return pullRequestMsg{key: key, err: err}
		}
		if _, err := runIn(ctx, repo, "git", "push", "-u", "origin", branch); err != nil {
			return pullRequestMsg{key: key, err: err}
		}
		var out string
		if strings.Contains(remote, "github") {
			out, err = runIn(ctx, repo, "gh", "pr", "create", "--draft", "--head", branch, "--title", title, "--body", body)
		} else {
			out, err = runIn(ctx, repo, "glab", "mr", "create", "--draft", "--source-branch", branch, "--title", title, "--description", body, "--yes")
		}
		if err != nil {
			return pullRequestMsg{key: key, err: err}
		}
		url := out
		if i := strings.LastIndex(out, "http"); i >= 0 {
			url = strings.Fields(out[i:])[0]
		}
		return pullRequestMsg{key: key, url: url}
	}
}

func (m Model) handlePullRequest(msg pullRequestMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.key + ": " + msg.err.Error())
		return m, nil
	}
	m.status = "draft opened for " + msg.key + ": " + msg.url + " · " + helpKey(m.keys.DevInfo) + " lists it"
	return m, nil
}
