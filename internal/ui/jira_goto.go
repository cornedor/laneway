package ui

import (
	"regexp"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// The # prompt: open any issue by key, on the board or not. A bare number
// takes the board's project.

var jiraKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

func (m *Model) openJiraGoto() {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "ABC-123, 123 or a URL"
	ti.CharLimit = 300
	ti.SetWidth(24)
	ti.Focus()
	m.jiraGotoInput = ti
	m.jiraGotoActive = true
	m.jiraGotoErr, m.jiraGotoKey = "", ""
}

// jiraBrowseRe finds the key in a pasted issue URL: …/browse/ABC-1, or a
// board's …?selectedIssue=ABC-1.
var jiraBrowseRe = regexp.MustCompile(`(?:/browse/|selectedIssue=)([A-Za-z][A-Za-z0-9_]*-[0-9]+)`)

// jiraGotoKey turns the typed (or pasted) text into an issue key, "" when
// it is none.
func jiraGotoKey(in, project string) string {
	if sm := jiraBrowseRe.FindStringSubmatch(in); sm != nil {
		in = sm[1]
	}
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
		if slices.ContainsFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == k }) {
			return m.gotoJiraKey(k)
		}
		// Off the board: look it up first, so a typo stays here to fix.
		m.jiraGotoKey, m.jiraGotoErr = k, ""
		client, ctx := m.jiraClient, m.ctx
		return m, func() tea.Msg {
			_, err := client.Get(ctx, k)
			return jiraGotoMsg{key: k, err: err}
		}
	}
	var cmd tea.Cmd
	m.jiraGotoInput, cmd = m.jiraGotoInput.Update(msg)
	m.jiraGotoErr = ""
	return m, cmd
}

// jiraGotoMsg is the # prompt's lookup of a key off the board.
type jiraGotoMsg struct {
	key string
	err error
}

// handleJiraGoto opens a key the lookup found (or the index has, offline);
// else the prompt stays, saying why.
func (m Model) handleJiraGoto(msg jiraGotoMsg) (tea.Model, tea.Cmd) {
	if !m.jiraGotoActive || msg.key != m.jiraGotoKey {
		return m, nil
	}
	m.jiraGotoKey = ""
	if _, ok := m.indexedIssue(msg.key, msg.err); msg.err != nil && !ok {
		m.jiraGotoErr = msg.err.Error()
		return m, nil
	}
	return m.gotoJiraKey(msg.key)
}

// gotoJiraKey closes the prompt and opens k in the panel.
func (m Model) gotoJiraKey(k string) (tea.Model, tea.Cmd) {
	m.jiraGotoActive = false
	m.selectJiraKey(k)
	m.renderJira()
	return m.openJiraKey(k)
}

func (m *Model) renderJiraGoto() string {
	inner := 32
	header := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).Render("Go to issue")
	hint := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).Render("↵ open · esc cancel")
	parts := []string{header, "", m.jiraGotoInput.View()}
	switch {
	case m.jiraGotoKey != "":
		parts = append(parts, refDimStyle.Render("looking up "+m.jiraGotoKey+"…"))
	case m.jiraGotoErr != "":
		parts = append(parts, lipgloss.NewStyle().Width(inner).Render(refErrStyle.Render(m.jiraGotoErr)))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, append(parts, "", hint)...)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).Render(body)
}
