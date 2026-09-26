package ui

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// copyJira puts an issue's key (y) or browse URL (Y) on the clipboard, over
// OSC 52 so it works across ssh.
func (m *Model) copyJira(issueKey string, url bool) tea.Cmd {
	if issueKey == "" {
		return nil
	}
	s := issueKey
	if url {
		s = m.jiraClient.BrowseURL(issueKey)
	}
	m.status = "copied " + s
	return tea.SetClipboard(s)
}

// Jira's REST API has no suggested branch name (its "create branch" dialog
// builds one in the browser), so ui.branch_template makes one.
const defaultBranchTemplate = "{key}-{summary}"

var branchPlaceholder = regexp.MustCompile(`\{[^}]*\}`)

// badBranchPlaceholder is the template's first unknown {…}, "" when none.
func badBranchPlaceholder(tmpl string) string {
	for _, p := range branchPlaceholder.FindAllString(tmpl, -1) {
		switch p {
		case "{key}", "{summary}", "{type}", "{project}":
		default:
			return p
		}
	}
	return ""
}

// branchName fills tmpl for an issue: the summary slugged and capped, the
// type slugged (Sub-task → sub-task).
func branchName(tmpl, issueKey, typ, summary string) string {
	project, _, _ := strings.Cut(issueKey, "-")
	if typ != "" {
		typ = slugify(typ)
	}
	return strings.NewReplacer(
		"{key}", issueKey,
		"{summary}", slugify(summary),
		"{type}", typ,
		"{project}", project,
	).Replace(tmpl)
}

// copyBranch puts the issue's branch name (ui.branch_template) on the
// clipboard.
func (m *Model) copyBranch(issueKey, typ, summary string) tea.Cmd {
	if issueKey == "" {
		return nil
	}
	s := branchName(m.opts.branchTemplate, issueKey, typ, summary)
	m.status = "copied " + s
	return tea.SetClipboard(s)
}

// copyJiraTable puts the marked cards on the clipboard as a markdown table,
// in list order; marked cards the list doesn't show (a search) follow.
func (m *Model) copyJiraTable() tea.Cmd {
	t := m.jiraTab
	var cards []jira.Card
	seen := map[string]bool{}
	for _, i := range t.order {
		if c := t.cards[i]; t.marked[c.Key] {
			cards = append(cards, c)
			seen[c.Key] = true
		}
	}
	for _, c := range t.cards {
		if t.marked[c.Key] && !seen[c.Key] {
			cards = append(cards, c)
		}
	}
	rows := make([][]string, len(cards))
	for i, c := range cards {
		rows[i] = []string{"[" + c.Key + "](" + m.jiraClient.BrowseURL(c.Key) + ")", c.Summary, c.Status, c.Assignee, c.Points}
	}
	m.status = fmt.Sprintf("copied %d rows as a markdown table", len(cards))
	return tea.SetClipboard(markdownTable([]string{"Key", "Summary", "Status", "Assignee", "Points"}, rows))
}
