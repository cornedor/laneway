package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"jiratui/internal/jira"
)

// The board's search: / narrows the loaded cards to those whose key, summary
// or assignee contain the query. It filters locally, without a refetch.

// startJiraSearch focuses the search box, keeping any query already there.
func (m *Model) startJiraSearch() {
	t := m.jiraTab
	if !t.searching {
		q := t.search.Value()
		t.search = textinput.New()
		t.search.Prompt = "/"
		t.search.Placeholder = "search key, summary, assignee"
		t.search.SetWidth(40)
		t.search.SetValue(q)
		t.search.CursorEnd()
	}
	t.searching = true
	t.search.Focus()
	m.renderJira()
}

// jiraSearchQuery is the active query, lowercased.
func (t *jiraTabState) jiraSearchQuery() string {
	return strings.ToLower(strings.TrimSpace(t.search.Value()))
}

// jiraCardMatches reports whether c matches the lowercased query q.
func jiraCardMatches(c jira.Card, q string) bool {
	if q == "" {
		return true
	}
	for _, s := range []string{c.Key, c.Summary, c.Assignee} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

// applyJiraSearch rebuilds the lanes for the current query, keeping the
// selection when the card still shows.
func (m *Model) applyJiraSearch() {
	keep := m.selectedJiraKey()
	m.buildJiraLanes()
	m.selectJiraKey(keep)
	m.renderJira()
}

// clearJiraSearch drops the query and closes the box.
func (m *Model) clearJiraSearch() {
	t := m.jiraTab
	t.searching = false
	t.search.SetValue("")
	t.search.Blur()
	m.applyJiraSearch()
}

// handleJiraSearchKey types into the search box: enter keeps the query and
// returns to the board, esc clears it, arrows still move the cursor.
func (m Model) handleJiraSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "esc":
		m.clearJiraSearch()
		return m, nil
	case msg.String() == "enter":
		t.searching = false
		t.search.Blur()
		if t.jiraSearchQuery() == "" {
			m.clearJiraSearch()
		} else {
			m.renderJira()
		}
		return m, nil
	case key.Matches(msg, m.keys.InputUp):
		m.moveJiraCursor(-1)
		return m, nil
	case key.Matches(msg, m.keys.InputDown):
		m.moveJiraCursor(1)
		return m, nil
	}
	before := t.search.Value()
	var cmd tea.Cmd
	t.search, cmd = t.search.Update(msg)
	if t.search.Value() != before {
		m.applyJiraSearch()
	}
	return m, cmd
}
