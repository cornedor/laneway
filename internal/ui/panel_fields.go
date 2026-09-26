package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// The panel's field cursor: tab / shift-tab walk the issue's editable fields,
// enter edits the selected one with the same editor as its own key. After the
// panel's own fields come the rest of the issue's edit screen (editmeta),
// edited with the transition form's per-kind editors and written at once.

// panelField is one editable field of the panel.
type panelField struct {
	name string // the label its row shows
	edit func(m *Model) tea.Cmd
}

// panelFields is set in init: its editors render the panel, which reads it.
var panelFields []panelField

func init() {
	panelFields = []panelField{
		{"Summary", func(m *Model) tea.Cmd { m.openJiraSummaryInput(); return nil }},
		{"Status", func(m *Model) tea.Cmd { return m.openJiraStatusPicker() }},
		{"Priority", func(m *Model) tea.Cmd { return m.openJiraPriorityPicker() }},
		{"Points", func(m *Model) tea.Cmd { m.openJiraPointsInput(); return nil }},
		{"Assignee", func(m *Model) tea.Cmd { return m.openJiraAssigneePicker() }},
		{"Labels", func(m *Model) tea.Cmd { m.openJiraLabelsInput(); return nil }},
	}
}

// panelExtraMsg is the shown issue's edit screen fetched.
type panelExtraMsg struct {
	key    string
	fields []jiraFormField
	err    error
}

// fetchPanelExtra loads the shown issue's other editable fields.
func (m *Model) fetchPanelExtra() tea.Cmd {
	if m.jiraIssue == nil || !m.jiraClient.Enabled() {
		return nil
	}
	c, ctx, key := m.jiraClient, m.ctx, m.jiraIssue.Key
	return func() tea.Msg {
		metas, values, err := c.EditMeta(ctx, key)
		fields := make([]jiraFormField, len(metas))
		for i, fm := range metas {
			fields[i] = jiraFormField{FieldMeta: fm, val: jira.DecodeValue(fm.Kind, values[fm.ID]), raw: values[fm.ID]}
		}
		return panelExtraMsg{key: key, fields: fields, err: err}
	}
}

// handlePanelExtra installs the fields when they are still the shown issue's.
// A failed fetch leaves the panel's own fields only.
func (m Model) handlePanelExtra(msg panelExtraMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || m.jiraIssue == nil || m.jiraIssue.Key != msg.key {
		return m, nil
	}
	m.panelExtra, m.panelExtraKey = msg.fields, msg.key
	m.renderRef()
	return m, nil
}

// extraFields is the shown issue's editmeta fields, none until they load.
func (m *Model) extraFields() []jiraFormField {
	if m.jiraIssue == nil || m.panelExtraKey != m.jiraIssue.Key {
		return nil
	}
	return m.panelExtra
}

func (m *Model) panelFieldCount() int { return len(panelFields) + len(m.extraFields()) }

// panelFieldIdx is the selected field's index (panelFields, then
// extraFields), -1 when none or the cursor belongs to another issue.
func (m *Model) panelFieldIdx() int {
	if m.jiraIssue == nil || m.fieldCursorKey != m.jiraIssue.Key || m.fieldCursor < 0 || m.fieldCursor >= m.panelFieldCount() {
		return -1
	}
	return m.fieldCursor
}

// panelFieldSel is the selected field's name, "" when none.
func (m *Model) panelFieldSel() string {
	i := m.panelFieldIdx()
	switch {
	case i < 0:
		return ""
	case i < len(panelFields):
		return panelFields[i].name
	}
	return m.extraFields()[i-len(panelFields)].Name
}

// panelFieldRow is the index of the panel's own field name, -1 when none.
func panelFieldRow(name string) int {
	return slices.IndexFunc(panelFields, func(f panelField) bool { return f.name == name })
}

// panelFieldIs reports whether the cursor is on the panel's own field name.
func (m *Model) panelFieldIs(name string) bool {
	i := m.panelFieldIdx()
	return i >= 0 && i < len(panelFields) && panelFields[i].name == name
}

// movePanelField steps the cursor by d; stepping off either end drops it,
// and tab past the last field hands focus to the board.
func (m *Model) movePanelField(d int) {
	i := m.panelFieldIdx()
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
	if i >= m.panelFieldCount() {
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
	i := m.panelFieldIdx()
	switch {
	case i < 0:
		return nil
	case i < len(panelFields):
		return panelFields[i].edit(m)
	}
	ff := m.extraFields()[i-len(panelFields)]
	m.panelEditID = ff.ID
	switch ff.Kind {
	case jira.KindDoc:
		key := m.jiraIssue.Key
		return func() tea.Msg {
			ed, err := jira.EditableDescription(ff.raw)
			return descLoadedMsg{key: key, field: ff.ID, md: ed.Markdown, kept: ed.Kept, err: err}
		}
	case jira.KindText, jira.KindNumber, jira.KindDate, jira.KindTime, jira.KindIssue:
		hint := ""
		switch ff.Kind {
		case jira.KindDate:
			hint = "2006-01-02, today, +3d, fri"
		case jira.KindTime:
			hint = "fri 14:00, 2026-10-01 9:30"
		case jira.KindIssue:
			hint = "issue key (empty clears)"
		}
		m.openJiraTextInput("field", ff.val.Text, hint, 0)
		m.startFieldInline(i)
		return nil
	}
	if ff.Kind == jira.KindSprint {
		ff.Options = m.sprintOptions()
	}
	cmd := m.openFieldPicker(ff, m.jiraIssue.Key)
	m.jiraPicker.inline = ff.ID
	return cmd
}

// sprintOptions are the board's sprints to pick, and none.
func (m *Model) sprintOptions() []jira.Option {
	opts := []jira.Option{{ID: "", Name: "none (backlog)"}}
	for _, v := range m.jiraTab.views {
		if v.kind == jiraViewSprint {
			opts = append(opts, jira.Option{ID: strconv.Itoa(v.sprint), Name: v.name})
		}
	}
	return opts
}

// panelEditField is the extra field being edited, zero when gone.
func (m *Model) panelEditField() jiraFormField {
	for _, ff := range m.extraFields() {
		if ff.ID == m.panelEditID {
			return ff
		}
	}
	return jiraFormField{}
}

// applyPanelExtraText writes the field input's text to the edited field.
func (m Model) applyPanelExtraText(raw string) (tea.Model, tea.Cmd) {
	ff := m.panelEditField()
	if ff.ID == "" {
		m.closeJiraField()
		return m, nil
	}
	if strings.TrimSpace(raw) == strings.TrimSpace(ff.val.Text) {
		m.closeJiraField()
		return m, nil
	}
	ff.val = jira.Value{Text: strings.TrimSpace(raw)}
	cmd, err := m.writePanelExtra(m.jiraFieldKey, ff)
	if err != nil {
		m.fail(ff.Name + ": " + err.Error())
		return m, nil
	}
	m.closeJiraField()
	return m, cmd
}

// pickPanelExtra applies a person or option picked for the edited field and
// writes it.
func (m *Model) pickPanelExtra(key string, kind jiraPickerKind, it jiraPickerItem) tea.Cmd {
	ff := m.panelEditField()
	if ff.ID == "" {
		return nil
	}
	// Copy the slices: the pick must not touch the shown value until Jira
	// has it.
	ff.val.Users = append([]jira.User(nil), ff.val.Users...)
	ff.val.Options = append([]jira.Option(nil), ff.val.Options...)
	pickFieldValue(&ff, kind, it)
	cmd, err := m.writePanelExtra(key, ff)
	if err != nil {
		m.fail(ff.Name + ": " + err.Error())
	}
	return cmd
}

// writePanelExtra sends ff's value; the reload after shows it.
func (m *Model) writePanelExtra(key string, ff jiraFormField) (tea.Cmd, error) {
	v, _, err := jira.EncodeValue(ff.Kind, ff.val)
	if err != nil {
		return nil, err
	}
	c, ctx, name := m.jiraClient, m.ctx, strings.ToLower(ff.Name)
	m.status = fmt.Sprintf("updating %s %s…", key, name)
	return jiraMutateCmd(key, name, func() error { return c.SetField(ctx, key, ff.ID, v) }), nil
}
