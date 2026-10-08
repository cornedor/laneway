package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// The panel's field cursor: tab / shift-tab walk the issue's editable fields,
// enter edits the selected one with the same editor as its own key. After the
// panel's own fields come the rest of the issue's edit screen (editmeta),
// edited with the transition form's per-kind editors and written at once:
// the starred ones (* on one, shared with the web) and filled rich text,
// then a More row (enter opens it, for the session) over the others.

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
		{"Reporter", func(m *Model) tea.Cmd { return m.openJiraReporterPicker() }},
		{"Labels", func(m *Model) tea.Cmd { m.openJiraLabelsInput(); return nil }},
	}
}

// panelExtraMsg is the shown issue's edit screen fetched.
type panelExtraMsg struct {
	key      string
	fields   []jiraFormField
	facts    jira.Facts
	err      error
	webLinks []jira.WebLink // its remote links; nil when they failed to load
	children []jira.Child   // an epic's child issues; nil for other issues
}

// fetchPanelExtra loads the shown issue's other editable fields.
func (m *Model) fetchPanelExtra() tea.Cmd {
	if m.jiraIssue == nil || !m.jiraClient.Enabled() {
		return nil
	}
	c, ctx, key, epic := m.jiraClient, m.ctx, m.jiraIssue.Key, m.isEpic(m.jiraIssue)
	return func() tea.Msg {
		_, _ = c.Myself(ctx)     // cached: which comments are yours to edit
		go c.WarmUsers(ctx, key) // @ in a comment answers at once
		links := make(chan []jira.WebLink, 1)
		go func() {
			l, _ := c.WebLinks(ctx, key) // a failure only leaves them out
			links <- l
		}()
		kids := make(chan []jira.Child, 1)
		go func() {
			var k []jira.Child
			if epic {
				k, _ = c.Children(ctx, key) // a failure only leaves them out
			}
			kids <- k
		}()
		metas, values, err := c.EditMeta(ctx, key)
		fields := make([]jiraFormField, len(metas))
		for i, fm := range metas {
			fields[i] = jiraFormField{FieldMeta: fm, val: jira.DecodeValue(fm.Kind, values[fm.ID]), raw: values[fm.ID]}
		}
		return panelExtraMsg{key: key, fields: fields, facts: jira.IssueFacts(values), err: err, webLinks: <-links, children: <-kids}
	}
}

// earlyExtra draws the shown issue's other fields from the edit screen last
// seen for its project and type (jira.Issue.Screen), till editmeta answers:
// no jump when it does.
func (m *Model) earlyExtra() {
	iss := m.jiraIssue
	if iss == nil || m.panelExtraKey == iss.Key || len(iss.Screen) == 0 {
		return
	}
	fields := make([]jiraFormField, len(iss.Screen))
	for i, fm := range iss.Screen {
		fields[i] = jiraFormField{FieldMeta: fm, val: iss.ScreenValues[fm.ID]}
	}
	m.panelExtra, m.panelExtraKey, m.panelExtraEarly = fields, iss.Key, true
}

// handlePanelExtra installs the fields when they are still the shown issue's.
// A failed fetch leaves the panel's own fields only.
func (m Model) handlePanelExtra(msg panelExtraMsg) (tea.Model, tea.Cmd) {
	if m.jiraIssue == nil || m.jiraIssue.Key != msg.key {
		return m, nil
	}
	m.webLinks, m.webLinksKey = msg.webLinks, msg.key
	m.children = msg.children
	if msg.err != nil {
		if m.panelExtraEarly { // the remembered screen stays, to read
			for i := range m.panelExtra {
				m.panelExtra[i].ReadOnly = true
			}
			m.panelExtraEarly = false
		}
		m.renderRef()
		return m, nil
	}
	m.panelExtra, m.panelExtraKey, m.panelFacts, m.panelExtraEarly = msg.fields, msg.key, msg.facts, false
	m.renderRef()
	return m, nil
}

// moreFieldsName is the More row's name for the field cursor.
var moreFieldsName = i18n.N("More fields")

// splitExtra is the shown issue's editmeta fields, none until they load:
// those always shown (starred, filled rich text) and the ones More folds.
func (m *Model) splitExtra() (top, rest []jiraFormField) {
	if m.jiraIssue == nil || m.panelExtraKey != m.jiraIssue.Key {
		return nil, nil
	}
	for _, ff := range m.panelExtra {
		if m.starred[ff.ID] || richField(ff) {
			top = append(top, ff)
		} else {
			rest = append(rest, ff)
		}
	}
	return top, rest
}

// foldedShown is the More fields an open More shows: all, or the filled
// ones with ui.empty_fields: hide.
func (m *Model) foldedShown() []jiraFormField {
	_, rest := m.splitExtra()
	if !m.moreFields {
		return nil
	}
	if !m.opts.hideEmpty || m.showEmpty {
		return rest
	}
	var out []jiraFormField
	for _, ff := range rest {
		if !ff.val.Empty() {
			out = append(out, ff)
		}
	}
	return out
}

// extraFields are the editmeta fields the panel shows, in its order.
func (m *Model) extraFields() []jiraFormField {
	top, _ := m.splitExtra()
	return append(top, m.foldedShown()...)
}

// moreRow is whether the panel has a More row: fields to fold.
func (m *Model) moreRow() bool {
	_, rest := m.splitExtra()
	return len(rest) > 0
}

// hiddenFields is how many empty fields ui.empty_fields: hide folds away
// in an open More.
func (m *Model) hiddenFields() int {
	if !m.moreFields {
		return 0
	}
	_, rest := m.splitExtra()
	return len(rest) - len(m.foldedShown())
}

func (m *Model) panelFieldCount() int {
	n := len(panelFields) + len(m.extraFields())
	if m.moreRow() {
		n++
	}
	return n
}

// panelSlot is what field cursor index i is on: an editmeta field, or the
// More row (more); nil and false for the panel's own fields.
func (m *Model) panelSlot(i int) (ff *jiraFormField, more bool) {
	j := i - len(panelFields)
	if j < 0 {
		return nil, false
	}
	top, _ := m.splitExtra()
	switch shown := m.foldedShown(); {
	case j < len(top):
		return &top[j], false
	case j == len(top) && m.moreRow():
		return nil, true
	case j-len(top)-1 < len(shown):
		return &shown[j-len(top)-1], false
	}
	return nil, false
}

// toggleStar stars the selected editmeta field, or takes its star off;
// it reports whether one was selected.
func (m *Model) toggleStar() bool {
	ff, _ := m.panelSlot(m.panelFieldIdx())
	if ff == nil || m.store == nil {
		return false
	}
	ids, err := jira.SetStarred(m.store, ff.ID, !m.starred[ff.ID])
	if err != nil {
		m.fail(i18n.Tf("star not kept: %s", err.Error()))
		return true
	}
	m.starred = map[string]bool{}
	for _, id := range ids {
		m.starred[id] = true
	}
	if m.starred[ff.ID] {
		m.status = i18n.Tf("%s starred: shown on every issue", ff.Name)
	} else {
		m.status = i18n.Tf("%s unstarred: under %s", ff.Name, i18n.T(moreFieldsName))
	}
	// the cursor follows the field to where it moved
	id := ff.ID
	for i := len(panelFields); i < m.panelFieldCount(); i++ {
		if f, _ := m.panelSlot(i); f != nil && f.ID == id {
			m.fieldCursor = i
		}
	}
	m.renderRef()
	return true
}

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
	if ff, more := m.panelSlot(i); more {
		return moreFieldsName
	} else if ff != nil {
		return ff.Name
	}
	return ""
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
	m.commentCursorKey = ""
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
	slot, more := m.panelSlot(i)
	if more {
		m.moreFields = !m.moreFields
		m.renderRef()
		return nil
	}
	if slot == nil {
		return nil
	}
	ff := *slot
	if m.panelExtraEarly {
		m.status = i18n.T("a moment: asking Jira what can be edited…")
		return nil
	}
	if ff.ReadOnly {
		m.status = i18n.Tf("Jira lets no one edit %s on %s now", ff.Name, m.jiraIssue.Key)
		return nil
	}
	m.panelEditID = ff.ID
	switch ff.Kind {
	case jira.KindDoc:
		key := m.jiraIssue.Key
		return func() tea.Msg {
			ed, err := jira.EditableDescription(ff.raw)
			return descLoadedMsg{key: key, field: ff.ID, md: ed.Markdown, kept: ed.Kept, err: err}
		}
	case jira.KindText, jira.KindStrings, jira.KindNumber, jira.KindDate, jira.KindTime, jira.KindIssue:
		hint := ""
		switch ff.Kind {
		case jira.KindDate:
			hint = i18n.T("2006-01-02, today, +3d, fri")
		case jira.KindTime:
			hint = i18n.T("fri 14:00, 2026-10-01 9:30")
		case jira.KindIssue:
			hint = i18n.T("issue key (empty clears)")
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
	opts := []jira.Option{{ID: "", Name: i18n.T("none (backlog)")}}
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
	m.status = fmt.Sprintf(i18n.T("updating %s %s…"), key, name)
	return jiraMutateCmd(key, name, func() error { return c.SetField(ctx, key, ff.ID, v) }), nil
}
