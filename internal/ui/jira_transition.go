package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// Moving an issue the way Jira's own board does: when the workflow insists on
// fields the issue lacks (a code reviewer for Code review, a comment for
// Waiting for), the move opens a form with the transition screen's fields
// instead of failing with the validator's message. The rules come from the
// workflow (jira.TransitionRules); when they can't be read, a move with a
// screen always shows it. A move whose rules are met goes straight through.

// jiraFormOrigin is where a move started, which decides what happens after:
// the board refetches its cards, the panel reloads its issue.
type jiraFormOrigin int

const (
	jiraFromBoard jiraFormOrigin = iota
	jiraFromPanel
)

// jiraFormField is one row of the form: a screen field, whether the workflow
// requires it, and its value (the issue's current one until edited).
type jiraFormField struct {
	jira.FieldMeta
	required bool
	val      jira.Value
	changed  bool
	raw      json.RawMessage // the value as Jira sent it (a doc field's ADF)
}

// jiraFormState is the open transition form. idx == len(fields) is the move
// button.
type jiraFormState struct {
	key          string
	transitionID string
	to           string
	origin       jiraFormOrigin
	message      string // the validators' own explanation
	fields       []jiraFormField
	idx          int
	editing      bool
	input        textinput.Model
	busy         bool
	err          string
	bulk         []string // marked cards the move goes to, with these fields (bulk.go)
	// firstRow is the first field's line inside the box, as last drawn; the
	// button sits a blank line after the last field (clickJiraForm).
	firstRow int
}

// jiraPreparedMsg is a move worked out: moved already (form nil), or waiting
// on the form.
type jiraPreparedMsg struct {
	key    string
	to     string
	origin jiraFormOrigin
	form   *jiraFormState
	err    error
}

// jiraFormDoneMsg is the form's move answered.
type jiraFormDoneMsg struct {
	key    string
	to     string
	origin jiraFormOrigin
	err    error
}

// prepareJiraMove finds the transition want picks for the issue, and either
// makes it (its required fields are filled) or returns the form for it.
func (m *Model) prepareJiraMove(key, to string, origin jiraFormOrigin, want func(jira.TransitionMeta) bool) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		msg := jiraPreparedMsg{key: key, to: to, origin: origin}
		var (
			metas      []jira.TransitionMeta
			ic         jira.IssueContext
			mErr, iErr error
			wg         sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); metas, mErr = c.TransitionsMeta(ctx, key) }()
		go func() { defer wg.Done(); ic, iErr = c.IssueContext(ctx, key) }()
		wg.Wait()
		if mErr != nil || iErr != nil {
			msg.err = firstErr(mErr, iErr)
			return msg
		}
		i := slices.IndexFunc(metas, want)
		if i < 0 {
			msg.err = fmt.Errorf("no transition to %s from here", to)
			return msg
		}
		t := metas[i]
		rules, rErr := c.TransitionRules(ctx, ic.Project, ic.TypeID)
		form := buildJiraForm(key, t, rules[t.ID], ic)
		form.origin = origin
		need := false
		for _, f := range form.fields {
			need = need || (f.required && f.val.Empty())
		}
		if rErr != nil && t.HasScreen && len(form.fields) > 0 {
			need = true // the rules are unknown: show the screen, as Jira does
		}
		if !need {
			msg.err = c.TransitionWith(ctx, key, t.ID, nil, "")
			return msg
		}
		msg.form = form
		return msg
	}
}

// firstErr is the first non-nil error.
func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// buildJiraForm lays out the form: the screen's fields, then any required
// field the screen lacks (the comment, usually), required ones first.
func buildJiraForm(key string, t jira.TransitionMeta, rule jira.TransitionRule, ic jira.IssueContext) *jiraFormState {
	f := &jiraFormState{key: key, transitionID: t.ID, to: t.ToName, message: rule.Message}
	for _, fm := range t.Fields {
		f.fields = append(f.fields, jiraFormField{FieldMeta: fm, required: slices.Contains(rule.Required, fm.ID),
			val: jira.DecodeValue(fm.Kind, ic.Values[fm.ID])})
	}
	for _, id := range rule.Required {
		if slices.ContainsFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == id }) {
			continue
		}
		fm := jira.FieldMeta{ID: id, Name: id, Kind: jira.KindOther}
		if id == jira.CommentField {
			fm.Name, fm.Kind = "Comment", jira.KindComment
		}
		ff := jiraFormField{FieldMeta: fm, required: true}
		if fm.Kind == jira.KindOther {
			ff.val = jira.DecodeValue(jira.KindOther, ic.Values[id])
		}
		f.fields = append(f.fields, ff)
	}
	slices.SortStableFunc(f.fields, func(a, b jiraFormField) int {
		switch {
		case a.required == b.required:
			return 0
		case a.required:
			return -1
		}
		return 1
	})
	// Start on the first thing to fill in.
	f.idx = slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.required && ff.val.Empty() })
	if f.idx < 0 {
		f.idx = 0
	}
	return f
}

// startJiraMoveFromPanel moves the panel's issue along a transition picked in
// the status picker.
func (m *Model) startJiraMoveFromPanel(key, transitionID, to string) tea.Cmd {
	m.status = fmt.Sprintf("moving %s → %s…", key, to)
	return m.prepareJiraMove(key, to, jiraFromPanel, func(t jira.TransitionMeta) bool { return t.ID == transitionID })
}

func (m Model) handleJiraPrepared(msg jiraPreparedMsg) (tea.Model, tea.Cmd) {
	if msg.form != nil {
		m.jiraForm = msg.form
		m.status = fmt.Sprintf("%s → %s needs a few fields", msg.key, msg.to)
		return m, nil
	}
	return m.finishJiraMove(msg.key, msg.to, msg.origin, msg.err)
}

// finishJiraMove reports a move and refreshes whatever shows the issue.
func (m Model) finishJiraMove(key, to string, origin jiraFormOrigin, err error) (tea.Model, tea.Cmd) {
	if origin == jiraFromBoard {
		return m.handleJiraMoved(jiraMovedMsg{key: key, lane: to, err: err})
	}
	return m.handleJiraMutated(jiraMutatedMsg{key: key, field: "status", err: err})
}

func (m Model) handleJiraFormDone(msg jiraFormDoneMsg) (tea.Model, tea.Cmd) {
	f := m.jiraForm
	if f != nil && f.key == msg.key && msg.err != nil {
		// Keep the form up with Jira's answer, so the fix is one edit away.
		f.busy = false
		f.err = msg.err.Error()
		return m, nil
	}
	m.jiraForm = nil
	return m.finishJiraMove(msg.key, msg.to, msg.origin, msg.err)
}

// cancelJiraForm drops the form; a board move is put back.
func (m *Model) cancelJiraForm() tea.Cmd {
	f := m.jiraForm
	m.jiraForm = nil
	m.status = f.key + ": move cancelled"
	if f.origin == jiraFromBoard {
		return m.loadJiraCards(m.jiraTab.viewIdx, false)
	}
	return nil
}

func (m Model) handleJiraFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.jiraForm
	if f.busy {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	if f.editing {
		switch msg.String() {
		case "enter":
			ff := &f.fields[f.idx]
			ff.val.Text = f.input.Value()
			ff.changed = true
			f.editing = false
			f.err = ""
			return m, nil
		case "esc":
			f.editing = false
			return m, nil
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return m, cmd
	}
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "esc":
		return m, m.cancelJiraForm()
	case msg.String() == "ctrl+s":
		return m, m.submitJiraForm()
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.InputUp), msg.String() == "shift+tab":
		f.idx = max(f.idx-1, 0)
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.InputDown), msg.String() == "tab":
		f.idx = min(f.idx+1, len(f.fields))
	case msg.String() == "delete", msg.String() == "backspace":
		if f.idx < len(f.fields) {
			ff := &f.fields[f.idx]
			ff.val, ff.changed = jira.Value{}, true
		}
	case msg.String() == "enter":
		if f.idx == len(f.fields) {
			return m, m.submitJiraForm()
		}
		return m, m.editJiraFormField()
	}
	return m, nil
}

// editJiraFormField starts editing the selected row: inline for text, a
// picker for people and options.
func (m *Model) editJiraFormField() tea.Cmd {
	f := m.jiraForm
	ff := &f.fields[f.idx]
	switch ff.Kind {
	case jira.KindText, jira.KindNumber, jira.KindDate, jira.KindTime, jira.KindIssue, jira.KindDoc, jira.KindComment:
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = strings.ToLower(ff.Name) + "…"
		ti.SetWidth(40)
		ti.SetValue(strings.ReplaceAll(ff.val.Text, "\n", " "))
		ti.CursorEnd()
		f.input = ti
		f.editing = true
		return f.input.Focus()
	case jira.KindUser, jira.KindUsers, jira.KindOption, jira.KindOptions:
		return m.openFieldPicker(*ff, f.key)
	case jira.KindSprint:
		ff.Options = m.sprintOptions()
		return m.openFieldPicker(*ff, f.key)
	default:
		f.err = ff.Name + " can't be set here — set it in Jira (esc, then o)"
	}
	return nil
}

// openFieldPicker opens the person or option picker for a field of key: the
// transition form's row, or the panel's (panel_fields.go).
func (m *Model) openFieldPicker(ff jiraFormField, key string) tea.Cmd {
	if ff.Kind == jira.KindUser || ff.Kind == jira.KindUsers {
		gen := m.startJiraPicker(jiraPickFormUser, ff.Name+" — "+key, true)
		m.jiraPicker.issueKey = key
		if len(ff.val.Users) > 0 {
			m.jiraPicker.curAssignee = ff.val.Users[0].AccountID
		}
		return m.fetchAssignees(gen, m.jiraPicker.fetchSeq, key, "")
	}
	m.startJiraPicker(jiraPickFormOption, ff.Name+" — "+key, true)
	m.jiraPicker.issueKey = key
	items := make([]jiraPickerItem, len(ff.Options))
	for i, o := range ff.Options {
		items[i] = jiraPickerItem{id: o.ID, label: o.Name,
			current: slices.ContainsFunc(ff.val.Options, func(v jira.Option) bool { return v.ID == o.ID })}
	}
	m.setJiraPickerItems(items)
	return nil
}

// pickJiraFormValue stores a person or option picked for the selected row.
func (m *Model) pickJiraFormValue(kind jiraPickerKind, it jiraPickerItem) {
	f := m.jiraForm
	if f == nil || f.idx >= len(f.fields) {
		return
	}
	f.err = ""
	pickFieldValue(&f.fields[f.idx], kind, it)
}

// pickFieldValue applies a pick to ff. A multi-value field toggles the pick in
// or out; "" clears a person field.
func pickFieldValue(ff *jiraFormField, kind jiraPickerKind, it jiraPickerItem) {
	ff.changed = true
	if kind == jiraPickFormUser {
		label := strings.TrimSuffix(strings.TrimPrefix(it.label, "Assign to me ("), ")")
		u := jira.User{AccountID: it.id, DisplayName: label}
		switch {
		case it.id == "":
			ff.val.Users = nil
		case ff.Kind == jira.KindUsers:
			if i := slices.IndexFunc(ff.val.Users, func(x jira.User) bool { return x.AccountID == it.id }); i >= 0 {
				ff.val.Users = slices.Delete(ff.val.Users, i, i+1)
			} else {
				ff.val.Users = append(ff.val.Users, u)
			}
		default:
			ff.val.Users = []jira.User{u}
		}
		return
	}
	o := jira.Option{ID: it.id, Name: it.label}
	if ff.Kind == jira.KindOptions {
		if i := slices.IndexFunc(ff.val.Options, func(x jira.Option) bool { return x.ID == it.id }); i >= 0 {
			ff.val.Options = slices.Delete(ff.val.Options, i, i+1)
		} else {
			ff.val.Options = append(ff.val.Options, o)
		}
		return
	}
	ff.val.Options = []jira.Option{o}
}

// submitJiraForm checks the required fields and sends the move with every
// field the form changed.
func (m *Model) submitJiraForm() tea.Cmd {
	f := m.jiraForm
	var missing []string
	fields := map[string]any{}
	comment := ""
	for _, ff := range f.fields {
		if ff.required && ff.val.Empty() {
			missing = append(missing, ff.Name)
			continue
		}
		if ff.Kind == jira.KindComment {
			comment = ff.val.Text
			continue
		}
		if !ff.changed {
			continue
		}
		v, ok, err := jira.EncodeValue(ff.Kind, ff.val)
		if err != nil {
			f.err = ff.Name + ": " + err.Error()
			return nil
		}
		if ok {
			fields[ff.ID] = v
		}
	}
	if len(missing) > 0 {
		f.err = "fill in " + strings.Join(missing, ", ")
		return nil
	}
	if len(f.bulk) > 0 {
		m.jiraForm = nil
		return m.bulkTransition(f.bulk, f.to, fields, comment)
	}
	f.busy = true
	f.err = ""
	c, ctx := m.jiraClient, m.ctx
	key, id, to, origin := f.key, f.transitionID, f.to, f.origin
	return func() tea.Msg {
		return jiraFormDoneMsg{key: key, to: to, origin: origin, err: c.TransitionWith(ctx, key, id, fields, comment)}
	}
}

// jiraValueText is a value as a form row shows it.
func jiraValueText(v jira.Value) string {
	switch {
	case len(v.Users) > 0:
		names := make([]string, len(v.Users))
		for i, u := range v.Users {
			names[i] = u.DisplayName
		}
		return strings.Join(names, ", ")
	case len(v.Options) > 0:
		names := make([]string, len(v.Options))
		for i, o := range v.Options {
			names[i] = o.Name
		}
		return strings.Join(names, ", ")
	}
	return strings.ReplaceAll(strings.TrimSpace(v.Text), "\n", " ")
}

// clickJiraForm acts on a click in the form: a row is selected by one
// click and edited by a second (or a double-click); the button moves. A
// value being typed is kept when the click goes elsewhere.
func (m Model) clickJiraForm(x, y, count int) (tea.Model, tea.Cmd) {
	f := m.jiraForm
	if f.busy {
		return m, nil
	}
	bodyH := m.bodyH()
	box := m.renderJiraForm()
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	top, left := (bodyH-h)/2, (m.width-w)/2
	if x < left || x >= left+w {
		return m, nil
	}
	row := y - top - f.firstRow
	i := -1
	switch {
	case row >= 0 && row < len(f.fields):
		i = row
	case row == len(f.fields)+1:
		i = len(f.fields)
	}
	if i < 0 || (f.editing && i == f.idx) {
		return m, nil
	}
	if f.editing {
		ff := &f.fields[f.idx]
		ff.val.Text, ff.changed, f.editing = f.input.Value(), true, false
	}
	again := i == f.idx || count == 2
	f.idx = i
	switch {
	case i == len(f.fields):
		return m, m.submitJiraForm()
	case again:
		return m, m.editJiraFormField()
	}
	return m, nil
}

// renderJiraForm draws the transition form as a modal, like the pickers.
func (m *Model) renderJiraForm() string {
	f := m.jiraForm
	if f == nil {
		return ""
	}
	outerW := min(max(confirmDialogMaxWidth+16, 40), m.width-4)
	inner := max(outerW-8, 1)
	center := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center)
	parts := []string{center.Bold(true).Render(f.key + " → " + f.to)}
	if f.message != "" {
		parts = append(parts, "", lipgloss.NewStyle().Width(inner).Foreground(dimColor).Italic(true).Render(f.message))
	}
	nameW := 0
	for _, ff := range f.fields {
		nameW = max(nameW, lipgloss.Width(ff.Name)+2)
	}
	cursor := lipgloss.NewStyle().Foreground(focusedColor).Bold(true)
	parts = append(parts, "")
	f.firstRow = 2 // the border and the padding
	for _, p := range parts {
		f.firstRow += lipgloss.Height(p)
	}
	for i, ff := range f.fields {
		name := ff.Name
		if ff.required {
			name += " *"
		}
		name += strings.Repeat(" ", max(nameW-lipgloss.Width(name), 0))
		var val string
		switch {
		case f.editing && i == f.idx:
			val = f.input.View()
		case ff.val.Empty() && ff.required:
			val = gitlabWarnStyle.Render("required")
		case ff.val.Empty():
			val = refDimStyle.Render("—")
		default:
			val = jiraValueText(ff.val)
		}
		val = ansi.Truncate(val, max(inner-2-nameW-2, 1), "…")
		if i == f.idx {
			parts = append(parts, cursor.Render("▸ "+name)+"  "+val)
		} else {
			parts = append(parts, "  "+name+"  "+val)
		}
	}
	button := "[ Move to " + f.to + " ]"
	if f.busy {
		button = "moving…"
	}
	if f.idx == len(f.fields) {
		button = cursor.Render("▸ " + button)
	} else {
		button = "  " + button
	}
	parts = append(parts, "", button)
	if f.err != "" {
		parts = append(parts, "", lipgloss.NewStyle().Width(inner).Render(refErrStyle.Render(f.err)))
	}
	hint := "↑/↓ field · ↵ edit · del clear · ctrl+s move · esc cancel"
	if f.editing {
		hint = "↵ keep · esc undo"
	}
	parts = append(parts, "", center.Foreground(dimColor).Italic(true).Render(hint))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
