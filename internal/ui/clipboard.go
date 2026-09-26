package ui

import (
	"fmt"
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
	cell := strings.NewReplacer("|", `\|`, "\n", " ").Replace
	var b strings.Builder
	b.WriteString("| Key | Summary | Status | Assignee | Points |\n|---|---|---|---|---|\n")
	for _, c := range cards {
		fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s | %s |\n", c.Key, m.jiraClient.BrowseURL(c.Key),
			cell(c.Summary), cell(c.Status), cell(c.Assignee), c.Points)
	}
	m.status = fmt.Sprintf("copied %d rows as a markdown table", len(cards))
	return tea.SetClipboard(b.String())
}
