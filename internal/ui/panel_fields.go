package ui

import (
	tea "charm.land/bubbletea/v2"
)

// The panel's field cursor: tab / shift-tab walk the issue's editable fields,
// enter edits the selected one with the same editor as its own key.

// panelField is one editable field of the panel.
type panelField struct {
	name string // the label its row shows
	edit func(m *Model) tea.Cmd
}

var panelFields = []panelField{
	{"Summary", func(m *Model) tea.Cmd { m.openJiraSummaryInput(); return nil }},
	{"Status", func(m *Model) tea.Cmd { return m.openJiraStatusPicker() }},
	{"Priority", func(m *Model) tea.Cmd { return m.openJiraPriorityPicker() }},
	{"Points", func(m *Model) tea.Cmd { m.openJiraPointsInput(); return nil }},
	{"Assignee", func(m *Model) tea.Cmd { return m.openJiraAssigneePicker() }},
	{"Labels", func(m *Model) tea.Cmd { m.openJiraLabelsInput(); return nil }},
}

// panelFieldSel is the selected field's name, "" when none or the cursor
// belongs to another issue.
func (m *Model) panelFieldSel() string {
	if m.jiraIssue == nil || m.fieldCursorKey != m.jiraIssue.Key || m.fieldCursor < 0 || m.fieldCursor >= len(panelFields) {
		return ""
	}
	return panelFields[m.fieldCursor].name
}

// movePanelField steps the cursor by d; stepping off either end drops it,
// and tab past the last field hands focus to the board.
func (m *Model) movePanelField(d int) {
	i := -1
	if m.panelFieldSel() != "" {
		i = m.fieldCursor
	}
	switch {
	case i < 0 && d > 0:
		i = 0
	case i < 0:
		m.focus = focusJira
		m.renderJira()
		return
	default:
		i += d
	}
	if i >= len(panelFields) {
		m.clearPanelField()
		m.focus = focusJira
		m.renderJira()
		return
	}
	if i < 0 {
		m.clearPanelField()
		return
	}
	m.fieldCursor, m.fieldCursorKey = i, m.jiraIssue.Key
	m.refView.GotoTop() // the fields sit at the top
	m.renderRef()
}

func (m *Model) clearPanelField() {
	m.fieldCursor, m.fieldCursorKey = -1, ""
	m.renderRef()
}

// editPanelField opens the selected field's editor.
func (m *Model) editPanelField() tea.Cmd {
	if m.panelFieldSel() == "" {
		return nil
	}
	return panelFields[m.fieldCursor].edit(m)
}
