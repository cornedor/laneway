package ui

import tea "charm.land/bubbletea/v2"

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
