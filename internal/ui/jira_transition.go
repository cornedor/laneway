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

	"github.com/cornedor/laneway/internal/editor"
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
	multiline    bool         // editing in area: a doc field or the comment
	area         editor.Model // keeps their lines, drawn under the row
	busy         bool
	err          string
	bulk         []string // marked cards the move goes to, with these fields (bulk.go)
	// work is start work's form (jira_work.go): which agent, the branch, the
	// prompt.
	work bool
	// create is the issue a failed create would make: the form asks for the
	// fields it lacked, and key is the create box's title (jira_create.go).
	create *jiraFormCreate
	// firstRow is the first field line's row inside the box, as last drawn;
	// rowField the field of each row from there (-1 none, editorRow the
	// description editor's lines) and buttonRow the button's row
	// (clickJiraForm).
	firstRow  int
	rowField  []int
	buttonRow int
	// top is the first field shown when the fields outgrow the screen.
	top int
}

// editorRow marks rows of the multiline editor in rowField.
const editorRow = -2

// jiraFormCreate is a create waiting on the form.
type jiraFormCreate struct {
	in      jira.NewIssue
	sprint  int
	cloneOf string // the issue a clone copies, linked once made
	// form is the create form itself (jira_create.go): type, summary and
	// description are its rows, not fields Jira asked for.
	form bool
	// discard is set by a first esc on a form with something typed.
	discard bool
	// fieldsSeq tags the create screen fetch for the type shown, loading
	// while it runs; kept holds what was typed in rows a type change hid.
	fieldsSeq int
	loading   bool
	kept      map[string]jira.Value
	// fieldErrs are Jira's reasons for refusing the create, by field id,
	// shown under the row while it holds the value refused.
	fieldErrs map[string]fieldErr
	// screen is the type's create screen, as last loaded.
	screen []jira.CreateField
	// mentions are the people @-completed in the description.
	mentions []jira.Mention
	// another keeps the form after this create, for the next one; made
	// are the keys it made so far.
	another bool
	made    []string
	// batch are the summaries of a pasted list, one issue each
	// (create_batch.go).
	batch []string
	// descKept are a clone's description blocks markdown can't hold, put
	// back when its description row is edited.
	descKept []json.RawMessage
}

// fieldErr is Jira's message about a field, and the value it was about.
type fieldErr struct{ msg, val string }

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
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(30*time.Second))
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
		ff := jiraFormField{FieldMeta: fm, required: slices.Contains(rule.Required, fm.ID),
			val: jira.DecodeValue(fm.Kind, ic.Values[fm.ID])}
		// A field the issue leaves empty starts at the screen's default,
		// sent with the move unless changed.
		if ff.val.Empty() && len(fm.Default) > 0 {
			if v := jira.DecodeValue(fm.Kind, fm.Default); !v.Empty() {
				ff.val, ff.changed = v, true
			}
		}
		f.fields = append(f.fields, ff)
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
	if f.create != nil {
		typed := slices.ContainsFunc(f.fields, func(ff jiraFormField) bool {
			return ff.ID != createTypeField && ff.changed && !ff.val.Empty()
		})
		if typed && !f.create.discard {
			m.jiraForm = f // kept: a second esc drops it
			f.create.discard = true
			f.err = "esc again drops what you typed"
			return nil
		}
		m.status = "create cancelled"
		return nil
	}
	if f.work {
		m.status = f.key + ": start work cancelled"
		return nil
	}
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
			return m.quit()
		}
		return m, nil
	}
	if f.editing && f.multiline {
		if m.mentionKey(msg.String()) { // an open list takes its keys first
			return m, nil
		}
		switch msg.String() {
		case "ctrl+s":
			ff := &f.fields[f.idx]
			ff.val.Text = strings.TrimRight(f.area.Value(), "\n")
			ff.changed = true
			f.editing, f.multiline = false, false
			f.err = ""
			m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
			return m, nil
		case "esc":
			f.editing, f.multiline = false, false
			m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
			return m, nil
		case "ctrl+c":
			return m.quit()
		}
		if d := m.formFieldStep(msg); d != 0 && m.multilineLeaves(msg, d) {
			ff := &f.fields[f.idx]
			ff.val.Text, ff.changed = strings.TrimRight(f.area.Value(), "\n"), true
			f.editing, f.multiline = false, false
			m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
			f.idx = min(max(f.idx+d, 0), len(f.fields))
			return m, nil
		}
		var cmd tea.Cmd
		f.area, cmd = f.area.Update(msg)
		return m, tea.Batch(cmd, m.scheduleMention())
	}
	if f.editing {
		if m.labelKey(msg) { // ↑ ↓ tab through a labels row's suggestions
			return m, nil
		}
		switch msg.String() {
		case "enter":
			ff := &f.fields[f.idx]
			ff.val.Text = f.input.Value()
			ff.changed = true
			f.editing = false
			f.err = ""
			if f.create != nil && f.create.form && ff.ID == createSummaryField {
				return m, m.submitJiraForm() // enter on the summary creates
			}
			return m, nil
		case "ctrl+s":
			ff := &f.fields[f.idx]
			ff.val.Text, ff.changed, f.editing = f.input.Value(), true, false
			return m, m.submitJiraForm()
		case "ctrl+enter", "alt+enter":
			if f.create != nil && f.create.form {
				ff := &f.fields[f.idx]
				ff.val.Text, ff.changed, f.editing = f.input.Value(), true, false
				return m, m.submitCreateAnother()
			}
		case "esc":
			f.editing = false
			m.labels.list = nil
			return m, nil
		}
		if d := m.formFieldStep(msg); d != 0 { // keep it and move on
			ff := &f.fields[f.idx]
			ff.val.Text, ff.changed, f.editing = f.input.Value(), true, false
			m.labels.list = nil
			f.idx = min(max(f.idx+d, 0), len(f.fields))
			return m, nil
		}
		before := f.input.Value()
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		if f.input.Value() != before {
			f.dropBatch()
			cmd = tea.Batch(cmd, m.suggestLabels())
		}
		return m, cmd
	}
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "esc":
		return m, m.cancelJiraForm()
	case msg.String() == "ctrl+s":
		return m, m.submitJiraForm()
	case (msg.String() == "ctrl+enter" || msg.String() == "alt+enter") && f.create != nil && f.create.form:
		return m, m.submitCreateAnother()
	case key.Matches(msg, m.keys.Up):
		f.idx = max(f.idx-1, 0)
	case key.Matches(msg, m.keys.Down):
		f.idx = min(f.idx+1, len(f.fields))
	case m.formFieldStep(msg) != 0:
		f.idx = min(max(f.idx+m.formFieldStep(msg), 0), len(f.fields))
	case f.create != nil && f.create.form && f.idx < len(f.fields) && f.fields[f.idx].ID == createTypeField &&
		(key.Matches(msg, m.keys.Left) || key.Matches(msg, m.keys.Right)):
		d := 1
		if key.Matches(msg, m.keys.Left) {
			d = -1
		}
		return m, m.cycleCreateType(d)
	case msg.String() == "delete", msg.String() == "backspace":
		if f.idx < len(f.fields) && f.fields[f.idx].ID != createMoreField {
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

// formFieldStep is the field a key moves a form to: 1 for the next (tab, ↓,
// ctrl+n), -1 for the previous (shift+tab, ↑, ctrl+p), 0 for any other key.
// j and k are not in it: while a field is edited they are letters.
func (m Model) formFieldStep(msg tea.KeyPressMsg) int {
	switch {
	case msg.String() == "tab", key.Matches(msg, m.keys.InputDown):
		return 1
	case msg.String() == "shift+tab", key.Matches(msg, m.keys.InputUp):
		return -1
	}
	return 0
}

// multilineLeaves is whether a step key leaves the multi-line field rather
// than moving in it: ↑ on its top row, ↓ on its bottom one, tab outside a
// table (where it steps cells).
func (m Model) multilineLeaves(msg tea.KeyPressMsg, d int) bool {
	a := &m.jiraForm.area
	switch s := msg.String(); {
	case s == "tab" || s == "shift+tab":
		return !a.InTableRow()
	case d < 0:
		return a.CursorVisualRow() == 0
	}
	return a.CursorOnLastVisualRow()
}

// editJiraFormField starts editing the selected row: inline for text, a
// picker for people and options.
func (m *Model) editJiraFormField() tea.Cmd {
	f := m.jiraForm
	ff := &f.fields[f.idx]
	if ff.ID == createMoreField {
		m.toggleCreateMore()
		return nil
	}
	if ff.ID == workActionsField {
		m.toggleWorkActions(ff)
		return nil
	}
	switch ff.Kind {
	case jira.KindDoc, jira.KindComment:
		ed := newModalComposer(strings.ToLower(ff.Name) + "…")
		ed.NativeCursor = false // drawn in the box: the caret is its own
		ed.MaxHeight = max(min(m.bodyH()-16, 10), 3)
		ed.SetValue(ff.val.Text)
		f.area = ed
		f.editing, f.multiline = true, true
		return nil
	case jira.KindText, jira.KindStrings, jira.KindNumber, jira.KindDate, jira.KindTime, jira.KindIssue:
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
		if f.create != nil {
			return m.openFieldPicker(*ff, f.create.in.Project) // people assignable in it
		}
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
// missingFields names the form's required rows still empty.
func missingFields(f *jiraFormState) []string {
	var out []string
	for _, ff := range f.fields {
		if ff.required && ff.val.Empty() {
			out = append(out, ff.Name)
		}
	}
	return out
}

// errFor is Jira's message about ff, while ff still holds what it refused.
func (c *jiraFormCreate) errFor(ff jiraFormField) (string, bool) {
	if c == nil {
		return "", false
	}
	fe, ok := c.fieldErrs[ff.ID]
	return fe.msg, ok && fe.val == jiraValueText(ff.val)
}

// submitCreateAnother creates the form's issue and keeps the form for the
// next one.
func (m *Model) submitCreateAnother() tea.Cmd {
	m.jiraForm.create.another = true
	cmd := m.submitJiraForm()
	if cmd == nil {
		m.jiraForm.create.another = false // it didn't go: missing fields
	}
	return cmd
}

func (m *Model) submitJiraForm() tea.Cmd {
	f := m.jiraForm
	if f.work {
		return m.submitWorkForm()
	}
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
		if f.create != nil && f.create.form && (ff.ID == createTypeField || ff.ID == createSummaryField || ff.ID == createDescField || ff.ID == createMoreField || ff.Kind == jira.KindSprint) {
			continue // the issue's own, see createFormIssue
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
	if f.create != nil {
		f.busy, f.err = true, ""
		cr := m.createFormIssue(f, fields)
		if cr.form && len(cr.batch) > 1 {
			return m.createBatch(cr)
		}
		title := ""
		if cr.form {
			m.rememberCreateType(cr.in.Project, cr.in.Type)
			title = f.key // what it lacks comes back as rows
		}
		return m.createJiraIssue(cr, title)
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
	case row >= 0 && row < len(f.rowField):
		i = f.rowField[row]
	case row == f.buttonRow:
		i = len(f.fields)
	}
	if i < 0 || (f.editing && i == f.idx) {
		return m, nil
	}
	if f.editing {
		ff := &f.fields[f.idx]
		ff.val.Text, ff.changed, f.editing = f.input.Value(), true, false
		if f.multiline {
			ff.val.Text, f.multiline = strings.TrimRight(f.area.Value(), "\n"), false
		}
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

// fieldWindow is the fields [lo, hi) that fit room lines, keeping the
// cursor's in view (the last ones with the button focused); f.top keeps the
// window still while the cursor moves inside it.
func (f *jiraFormState) fieldWindow(blocks [][]string, room int) (lo, hi int) {
	total := 0
	for _, b := range blocks {
		total += len(b)
	}
	if total <= room {
		f.top = 0
		return 0, len(blocks)
	}
	room = max(room-2, 1) // the ↑ and ↓ lines
	cur := min(f.idx, len(blocks)-1)
	lo = min(f.top, cur)
	used := func(a, b int) (n int) {
		for _, bl := range blocks[a:b] {
			n += len(bl)
		}
		return n
	}
	for lo < cur && used(lo, cur+1) > room {
		lo++
	}
	hi = cur + 1
	for hi < len(blocks) && used(lo, hi+1) <= room {
		hi++
	}
	for lo > 0 && used(lo-1, hi) <= room {
		lo--
	}
	f.top = lo
	return lo, hi
}

// renderJiraForm draws the transition form as a modal, like the pickers.
func (m *Model) renderJiraForm() string {
	m.formArea = nil
	f := m.jiraForm
	if f == nil {
		return ""
	}
	outerW := min(max(confirmDialogMaxWidth+16, 40), m.width-4)
	inner := max(outerW-8, 1)
	center := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center)
	title, button, busy := f.key+" → "+f.to, "[ Move to "+f.to+" ]", "moving…"
	if f.create != nil {
		title, button, busy = f.key, "[ Create ]", "creating…"
	}
	if f.work {
		title, button, busy = "Start work on "+f.key, "[ Start work ]", "starting…"
	}
	parts := []string{center.Bold(true).Render(title)}
	onCreate := f.create != nil && f.create.form
	if onCreate && f.err != "" { // about the whole create: first thing read
		parts = append(parts, "", lipgloss.NewStyle().Width(inner).Render(refErrStyle.Render(f.err)))
	}
	if f.message != "" {
		parts = append(parts, "", lipgloss.NewStyle().Width(inner).Foreground(dimColor).Italic(true).Render(f.message))
	}
	nameW := 0
	for _, ff := range f.fields {
		if ff.ID != createMoreField {
			nameW = max(nameW, lipgloss.Width(ff.Name)+2)
		}
	}
	cursor := lipgloss.NewStyle().Foreground(focusedColor).Bold(true)
	parts = append(parts, "")
	// Each field's lines, windowed below once the rest is measured.
	blocks := make([][]string, len(f.fields))
	areaAt := -1 // the editor's first line in its block
	for i, ff := range f.fields {
		if ff.ID == createMoreField {
			label := refDimStyle.Render(ff.Name)
			if i == f.idx {
				label = cursor.Render("▸ " + ff.Name)
			} else {
				label = "  " + label
			}
			blocks[i] = []string{label}
			continue
		}
		name := ff.Name
		if ff.required {
			name += " *"
		}
		name += strings.Repeat(" ", max(nameW-lipgloss.Width(name), 0))
		var val string
		switch {
		case ff.ID == workActionsField && ff.val.Empty():
			val = "[ ] " + refDimStyle.Render(strings.Join(m.startActions(f.key), " · "))
		case ff.ID == workActionsField:
			val = "[x] " + ff.val.Text
		case f.multiline && i == f.idx:
			f.area.SetWidth(max(inner-4, 8))
			blocks[i] = []string{cursor.Render("▸ " + name)}
			areaAt = 1
			for _, l := range strings.Split(f.area.View(), "\n") {
				blocks[i] = append(blocks[i], "  "+l)
			}
			continue
		case f.editing && i == f.idx && (ff.ID == "labels" || ff.Clause != ""):
			f.input.SetWidth(max(inner-2-nameW-3, 8))
			blocks[i] = append([]string{cursor.Render("▸ "+name) + "  " + f.input.View()}, m.labelLines(nameW+4)...)
			continue
		case f.editing && i == f.idx:
			f.input.SetWidth(max(inner-2-nameW-3, 8)) // its cell, so the cursor stays in view
			val = f.input.View()
		case ff.val.Empty() && ff.required:
			val = gitlabWarnStyle.Render("required")
		case ff.val.Empty():
			val = refDimStyle.Render("—")
		default:
			val = jiraValueText(ff.val)
			if lines := strings.Split(strings.TrimSpace(ff.val.Text), "\n"); len(ff.val.Users)+len(ff.val.Options) == 0 && len(lines) > 1 {
				more := " +1 line"
				if len(lines) > 2 {
					more = fmt.Sprintf(" +%d lines", len(lines)-1)
				}
				more = refDimStyle.Render(more)
				val = ansi.Truncate(lines[0], max(inner-2-nameW-2-lipgloss.Width(more), 1), "…") + more
			}
		}
		val = ansi.Truncate(val, max(inner-2-nameW-2, 1), "…")
		if i == f.idx {
			blocks[i] = []string{cursor.Render("▸ "+name) + "  " + val}
		} else {
			blocks[i] = []string{"  " + name + "  " + val}
		}
		if fe, ok := f.create.errFor(ff); ok {
			blocks[i] = append(blocks[i], strings.Repeat(" ", nameW+4)+refErrStyle.Render(ansi.Truncate(fe, max(inner-nameW-4, 1), "…")))
		}
		if ff.Hint != "" && i == f.idx {
			blocks[i] = append(blocks[i], strings.Repeat(" ", nameW+4)+refDimStyle.Italic(true).Render(ansi.Truncate(ff.Hint, max(inner-nameW-4, 1), "…")))
		}
	}
	if f.busy {
		button = busy
	}
	if f.idx == len(f.fields) {
		button = cursor.Render("▸ " + button)
	} else {
		button = "  " + button
	}
	var foot []string
	if f.create != nil && f.create.loading {
		foot = append(foot, refDimStyle.Render("  "+createFormType(f)+"'s fields loading…"))
	}
	foot = append(foot, "", button)
	if f.err != "" && !onCreate {
		foot = append(foot, "", lipgloss.NewStyle().Width(inner).Render(refErrStyle.Render(f.err)))
	}
	hint := "tab/↑↓ field · ↵ edit · del clear · ctrl+s move · esc cancel"
	if f.create != nil {
		hint = "tab/↑↓ field · ↵ edit · del clear · ctrl+s create · esc back"
	}
	if f.work {
		hint = "tab/↑↓ field · ↵ edit · del clear · ctrl+s start · esc cancel"
	}
	if onCreate {
		hint = "tab/↑↓ field · ↵ edit · ← → type · ctrl+s create · alt+↵ create another · esc cancel"
		if missing := missingFields(f); len(missing) > 0 {
			hint = "fill in " + strings.Join(missing, ", ") + " to create · tab/↑↓ field · esc cancel"
		}
	}
	switch {
	case f.multiline && onCreate && f.fields[f.idx].ID == createDescField:
		hint = "ctrl+s keep · tab field · ↵ newline · @ mention · : emoji · esc undo"
	case f.multiline:
		hint = "ctrl+s keep · tab field · ↵ newline · esc undo"
	case f.editing && onCreate && f.fields[f.idx].ID == createSummaryField:
		hint = "↵ create · alt+↵ create another · tab/↑↓ field · esc undo"
	case f.editing:
		hint = "↵ keep · tab/↑↓ field · esc undo"
	case f.work && f.idx < len(f.fields) && f.fields[f.idx].ID == workActionsField:
		hint = "tab/↑↓ field · ↵ toggle · ctrl+s start · esc cancel"
	}
	foot = append(foot, "", center.Foreground(dimColor).Italic(true).Render(hint))

	// The fields that fit around the cursor, the rest behind ↑/↓ counts.
	f.firstRow = 2 // the border and the padding
	for _, p := range parts {
		f.firstRow += lipgloss.Height(p)
	}
	room := m.bodyH() - f.firstRow - 2 - lipgloss.Height(strings.Join(foot, "\n"))
	lo, hi := f.fieldWindow(blocks, room)
	f.rowField = nil
	if lo > 0 {
		parts = append(parts, refDimStyle.Render(fmt.Sprintf("  ↑ %d more", lo)))
		f.rowField = append(f.rowField, -1)
	}
	for i := lo; i < hi; i++ {
		for j, l := range blocks[i] {
			if i == f.idx && areaAt >= 0 && j == areaAt {
				m.formArea = &point{x: 1 + 3 + 2, y: f.firstRow + len(f.rowField)} // border, padding, "  "
			}
			parts = append(parts, l)
			if i == f.idx && areaAt >= 0 && j >= areaAt {
				f.rowField = append(f.rowField, editorRow)
			} else {
				f.rowField = append(f.rowField, i)
			}
		}
	}
	if hi < len(blocks) {
		parts = append(parts, refDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(blocks)-hi)))
		f.rowField = append(f.rowField, -1)
	}
	f.buttonRow = len(f.rowField) + slices.Index(foot, button)
	parts = append(parts, foot...)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
