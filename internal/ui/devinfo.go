package ui

import (
	"context"
	"fmt"
	"github.com/cornedor/laneway/internal/i18n"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/jira"
)

// D in the panel: the pull requests (open first), builds, deployments,
// branches and commits linked to the issue; enter opens one in the browser,
// ctrl+y copies its branch (else its link).

func (m *Model) openDevInfo() tea.Cmd {
	if m.jiraIssue == nil {
		return nil
	}
	key := m.jiraIssue.Key
	gen := m.startJiraPicker(jiraPickDev, i18n.Tf("Development — %s", key), true)
	seq := m.jiraPicker.fetchSeq
	c, ctx, gl := m.jiraClient, m.ctx, m.gitlab
	return func() tea.Msg {
		items, err := c.DevInfo(ctx, key)
		if err == nil {
			items = DevWithGitLab(ctx, items, gl, key)
		}
		rows := make([]jiraPickerItem, len(items))
		for i, d := range items {
			label := i18n.Tf("branch  %s", d.Name)
			switch d.Kind {
			case "pr":
				label = fmt.Sprintf("%-8s %s%s  (%s)", d.Status, prNumber(d.URL), d.Name, d.Branch)
			case "build":
				label = i18n.Tf("%-8s build %s", d.Status, d.Name)
				if d.Branch != "" {
					label += "  (" + d.Branch + ")"
				}
			case "deploy":
				label = i18n.Tf("%-8s deploy %s → %s", d.Status, d.Name, d.Branch)
			case "commit":
				label = i18n.Tf("commit  %s  — %s", d.Name, d.Status)
			}
			if d.Repo != "" {
				label += "  " + d.Repo
			}
			branch, _, _ := strings.Cut(d.Branch, " → ") // a pull request's source
			switch d.Kind {
			case "branch":
				branch = d.Name
			case "deploy", "commit":
				branch = "" // an environment, none
			}
			rows[i] = jiraPickerItem{id: d.URL, label: label, value: branch}
		}
		if err == nil && len(rows) == 0 {
			rows = []jiraPickerItem{{label: i18n.T("no development work linked")}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickDev, items: rows, err: err}
	}
}

// prNumberRe finds a pull or merge request's number in its link.
var prNumberRe = regexp.MustCompile(`/(-/merge_requests|pull|pull-requests)/(\d+)`)

// prNumber is "!87 " for a merge request's link, "#12 " for a pull
// request's, "" for neither.
func prNumber(link string) string {
	m := prNumberRe.FindStringSubmatch(link)
	switch {
	case m == nil:
		return ""
	case m[1] == "-/merge_requests":
		return "!" + m[2] + " "
	}
	return "#" + m[2] + " "
}

// gitlabSearchTimeout bounds the GitLab search for an issue's merge requests.
const gitlabSearchTimeout = 8 * time.Second

// DevWithGitLab is items with the GitLab merge requests naming key added as
// pull requests, when Jira's development info lists none (no GitLab for
// Jira integration). The TUI's D and the web's development section share it.
func DevWithGitLab(ctx context.Context, items []jira.DevItem, s *gitlab.Sites, key string) []jira.DevItem {
	if s == nil || slices.ContainsFunc(items, func(d jira.DevItem) bool { return d.Kind == "pr" }) {
		return items
	}
	ctx, cancel := context.WithTimeout(ctx, gitlabSearchTimeout)
	defer cancel()
	for _, mr := range s.Search(ctx, key) {
		items = append(items, jira.DevItem{Kind: "pr", Name: mr.Title, Status: devStatus(mr), Repo: mr.Repo,
			Branch: mr.SourceBranch + " → " + mr.TargetBranch, URL: mr.WebURL, Tool: "GitLab", Author: mr.Author,
			Updated: mr.UpdatedAt, Source: mr.SourceBranch, Target: mr.TargetBranch})
	}
	return items
}

// devStatus is a merge request's state in dev-status words.
func devStatus(mr *forge.Change) string {
	switch {
	case mr.State == forge.StateMerged:
		return "MERGED"
	case mr.State == forge.StateClosed:
		return "DECLINED"
	case mr.Draft:
		return "DRAFT"
	}
	return "OPEN"
}
