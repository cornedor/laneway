package ui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// n on the board: pick an issue type, type a summary, and the issue is
// created in the board's project, in the sprint when a sprint is showing.

// jiraCreatedMsg reports a create.
type jiraCreatedMsg struct {
	key string
	err error
}

// openJiraCreate lists the project's issue types.
func (m *Model) openJiraCreate() tea.Cmd {
	project := m.jiraTab.project
	if project == "" {
		return nil
	}
	gen := m.startJiraPicker(jiraPickCreateType, "New issue in "+project, false)
	m.jiraCreateParent, m.jiraCreateProject = "", ""
	seq, c, ctx := m.jiraPicker.fetchSeq, m.jiraClient, m.ctx
	return func() tea.Msg {
		types, err := c.IssueTypes(ctx, project)
		items := make([]jiraPickerItem, len(types))
		for i, t := range types {
			items[i] = jiraPickerItem{id: t.Name, label: jiraTypeIcon(t.Name) + " " + t.Name}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickCreateType, items: items, err: err}
	}
}

// openJiraCreateSummary asks for the summary of a new issue of type typ.
func (m *Model) openJiraCreateSummary(typ string) {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "summary"
	ti.CharLimit = 255
	ti.SetWidth(56)
	ti.Focus()
	m.jiraCreateInput = ti
	m.jiraCreateType = typ
	m.jiraCreateActive = true
}

func (m Model) handleJiraCreateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.jiraCreateActive = false
		return m, nil
	case "enter":
		in := jira.NewIssue{Project: m.jiraTab.project, Type: m.jiraCreateType, Summary: m.jiraCreateInput.Value(),
			Parent: m.jiraCreateParent}
		if m.jiraCreateParent != "" {
			in.Project = m.jiraCreateProject
		}
		if in.Summary == "" {
			return m, nil
		}
		m.jiraCreateActive = false
		sprint := 0
		if v, ok := m.jiraCurrentView(); ok && v.kind == jiraViewSprint && in.Parent == "" && !strings.EqualFold(in.Type, "epic") {
			sprint = v.sprint
		}
		m.status = "creating " + in.Type + " in " + in.Project + "…"
		c, ctx := m.jiraClient, m.ctx
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			key, err := c.CreateIssue(ctx, in)
			if err == nil && sprint != 0 {
				if err = c.MoveToSprint(ctx, sprint, key); err != nil {
					err = &jiraCreateSprintErr{err}
				}
			}
			return jiraCreatedMsg{key: key, err: err}
		}
	}
	var cmd tea.Cmd
	m.jiraCreateInput, cmd = m.jiraCreateInput.Update(msg)
	return m, cmd
}

// jiraCreateSprintErr is a create that worked but did not reach the sprint.
type jiraCreateSprintErr struct{ err error }

func (e *jiraCreateSprintErr) Error() string { return "not added to the sprint: " + e.err.Error() }

// handleJiraCreated opens the new issue and refetches the board.
func (m Model) handleJiraCreated(msg jiraCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.key == "" {
		m.status = "create: " + msg.err.Error()
		return m, nil
	}
	refresh := m.refreshJiraAfterEdit()
	if m.jiraCreateReload && m.jiraTab.roadmap != nil {
		m.jiraCreateReload = false
		refresh = tea.Batch(refresh, m.loadRoadmap())
	}
	out, cmd := m.openJiraKey(msg.key)
	om := out.(Model)
	om.status = "created " + msg.key
	if msg.err != nil {
		om.status += ", " + msg.err.Error()
	}
	return om, tea.Batch(cmd, refresh)
}

func (m *Model) renderJiraCreate() string {
	inner := 62
	header := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).
		Render(m.jiraCreateTitle())
	hint := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).Render("↵ create · esc cancel")
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", m.jiraCreateInput.View(), "", hint)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).Render(body)
}

// jiraCreateTitle is "New Bug in ABC", or "New Sub-task of ABC-1".
func (m *Model) jiraCreateTitle() string {
	if m.jiraCreateParent != "" {
		return "New " + m.jiraCreateType + " of " + m.jiraCreateParent
	}
	return "New " + m.jiraCreateType + " in " + m.jiraTab.project
}
