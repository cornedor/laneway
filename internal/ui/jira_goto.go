package ui

import (
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The # prompt: open any issue by key, on the board or not. A bare number
// takes the board's project.

var jiraKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

func (m *Model) openJiraGoto() {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "ABC-123 or 123"
	ti.CharLimit = 32
	ti.SetWidth(24)
	ti.Focus()
	m.jiraGotoInput = ti
	m.jiraGotoActive = true
}

// jiraGotoKey turns the typed text into an issue key, "" when it is none.
func jiraGotoKey(in, project string) string {
	k := strings.ToUpper(strings.TrimSpace(in))
	if k != "" && strings.Trim(k, "0123456789") == "" && project != "" {
		k = project + "-" + k
	}
	if !jiraKeyRe.MatchString(k) {
		return ""
	}
	return k
}

func (m Model) handleJiraGotoKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.jiraGotoActive = false
		return m, nil
	case "enter":
		k := jiraGotoKey(m.jiraGotoInput.Value(), m.jiraTab.project)
		if k == "" {
			m.status = "not an issue key: " + m.jiraGotoInput.Value()
			return m, nil
		}
		m.jiraGotoActive = false
		m.selectJiraKey(k)
		m.renderJira()
		return m.openJiraKey(k)
	}
	var cmd tea.Cmd
	m.jiraGotoInput, cmd = m.jiraGotoInput.Update(msg)
	return m, cmd
}

func (m *Model) renderJiraGoto() string {
	inner := 32
	header := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).Render("Go to issue")
	hint := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).Render("↵ open · esc cancel")
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", m.jiraGotoInput.View(), "", hint)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).Render(body)
}
