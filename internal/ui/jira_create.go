package ui

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// n on the board: one form with the issue's type, summary and description,
// created in the board's project, in the sprint when a sprint is showing.
// (A subtask, an epic child and a roadmap epic still pick a type, then
// type a summary.)

// jiraCreatedMsg reports a create.
type jiraCreatedMsg struct {
	key  string
	err  error
	form *jiraFormState // the fields a failed create lacks, to fill in
}

// The create form's own rows, written to the issue itself rather than as
// fields.
const (
	createTypeField    = "issuetype"
	createSummaryField = "summary"
	createDescField    = "description"
)

// jiraCreateTypesMsg is a project's issue types, to open the create form.
type jiraCreateTypesMsg struct {
	project string
	types   []jira.Option
	err     error
}

// openJiraCreate loads the project's issue types for the create form.
func (m *Model) openJiraCreate() tea.Cmd {
	project := m.jiraTab.project
	if project == "" {
		return nil
	}
	m.jiraCreateParent, m.jiraCreateProject = "", ""
	m.status = "new issue in " + project + "…"
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		types, err := c.IssueTypes(ctx, project)
		return jiraCreateTypesMsg{project: project, types: types, err: err}
	}
}

// handleJiraCreateTypes opens the create form: the type you last made in
// the project (else Task, else the first), cursor in the summary.
func (m Model) handleJiraCreateTypes(msg jiraCreateTypesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || len(msg.types) == 0 {
		m.fail("new issue: " + cmp.Or(errText(msg.err), "no issue types in "+msg.project))
		return m, nil
	}
	if m.modalOpen() || msg.project != m.jiraTab.project {
		return m, nil
	}
	opts := make([]jira.Option, len(msg.types))
	for i, t := range msg.types {
		opts[i] = jira.Option{ID: t.Name, Name: t.Name}
	}
	typ := opts[0].Name
	for _, want := range []string{m.lastCreateType[msg.project], "Task"} {
		if i := slices.IndexFunc(opts, func(o jira.Option) bool { return strings.EqualFold(o.Name, want) }); want != "" && i >= 0 {
			typ = opts[i].Name
			break
		}
	}
	m.jiraCreateType = typ
	f := &jiraFormState{key: m.jiraCreateTitle(), idx: 1,
		create: &jiraFormCreate{in: jira.NewIssue{Project: msg.project}, form: true}}
	f.fields = []jiraFormField{
		{FieldMeta: jira.FieldMeta{ID: createTypeField, Name: "Type", Kind: jira.KindOption, Options: opts}, required: true,
			val: jira.Value{Options: []jira.Option{{ID: typ, Name: typ}}}},
		{FieldMeta: jira.FieldMeta{ID: createSummaryField, Name: "Summary", Kind: jira.KindText}, required: true},
		{FieldMeta: jira.FieldMeta{ID: createDescField, Name: "Description", Kind: jira.KindDoc},
			val: jira.Value{Text: m.opts.templates[strings.ToLower(typ)]}},
	}
	m.jiraForm = f
	m.status = ""
	return m, tea.Batch(m.editJiraFormField(), m.loadCreateFields())
}

// createFieldsMsg is a create screen fetched for the form's type.
type createFieldsMsg struct {
	project, typ string
	seq          int
	fields       []jira.CreateField
	err          error
}

// loadCreateFields shows the fields the form's type requires: at once when
// known, else once fetched (a later type change drops the fetch).
func (m *Model) loadCreateFields() tea.Cmd {
	f := m.jiraForm
	project, typ := f.create.in.Project, createFormType(f)
	if fields, ok := m.createFieldCache[project+"/"+typ]; ok {
		f.create.loading = false
		m.setCreateRows(fields)
		return nil
	}
	f.create.fieldsSeq++
	f.create.loading = true
	seq, c, ctx := f.create.fieldsSeq, m.jiraClient, m.ctx
	return func() tea.Msg {
		fields, err := c.CreateFields(ctx, project, typ)
		return createFieldsMsg{project: project, typ: typ, seq: seq, fields: fields, err: err}
	}
}

// handleCreateFields adds the required fields to the form still showing
// that type. A failed fetch leaves the form as it is: Jira's refusal still
// names what's missing.
func (m Model) handleCreateFields(msg createFieldsMsg) (tea.Model, tea.Cmd) {
	f := m.jiraForm
	if f == nil || f.create == nil || !f.create.form || msg.seq != f.create.fieldsSeq {
		return m, nil
	}
	f.create.loading = false
	if msg.err != nil {
		return m, nil
	}
	if m.createFieldCache == nil {
		m.createFieldCache = map[string][]jira.CreateField{}
	}
	m.createFieldCache[msg.project+"/"+msg.typ] = msg.fields
	m.setCreateRows(msg.fields)
	return m, nil
}

// setCreateRows puts the fields Jira requires of the type after the form's
// own rows, each with what was typed in it before, even under another type.
func (m *Model) setCreateRows(fields []jira.CreateField) {
	f := m.jiraForm
	if f.create.kept == nil {
		f.create.kept = map[string]jira.Value{}
	}
	own := f.fields[:0:0]
	for _, ff := range f.fields {
		switch ff.ID {
		case createTypeField, createSummaryField, createDescField:
			own = append(own, ff)
		default:
			f.create.kept[ff.ID] = ff.val
		}
	}
	for _, cf := range fields {
		switch cf.ID {
		case "project", createTypeField, createSummaryField, createDescField, "parent":
			continue
		}
		if !cf.Required {
			continue
		}
		v, typed := f.create.kept[cf.ID]
		own = append(own, jiraFormField{FieldMeta: cf.FieldMeta, required: true, val: v, changed: typed && !v.Empty()})
	}
	f.fields = own
	f.idx = min(f.idx, len(f.fields))
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// createFormType is the create form's type.
func createFormType(f *jiraFormState) string {
	for _, ff := range f.fields {
		if ff.ID == createTypeField && len(ff.val.Options) > 0 {
			return ff.val.Options[0].Name
		}
	}
	return ""
}

// syncCreateType follows a new type in the create form: the title (it may
// no longer join the sprint), the description while it is still the old
// type's template, and the fields the type requires.
func (m *Model) syncCreateType() tea.Cmd {
	f := m.jiraForm
	if f == nil || f.create == nil || !f.create.form {
		return nil
	}
	old, typ := m.jiraCreateType, createFormType(f)
	m.jiraCreateType = typ
	f.key = m.jiraCreateTitle()
	for i := range f.fields {
		if ff := &f.fields[i]; ff.ID == createDescField && ff.val.Text == m.opts.templates[strings.ToLower(old)] {
			ff.val.Text = m.opts.templates[strings.ToLower(typ)]
		}
	}
	return m.loadCreateFields()
}

// cycleCreateType steps the create form's type by d, as ← → do on its row.
func (m *Model) cycleCreateType(d int) tea.Cmd {
	ff := &m.jiraForm.fields[m.jiraForm.idx]
	i := slices.IndexFunc(ff.Options, func(o jira.Option) bool { return o.Name == createFormType(m.jiraForm) })
	o := ff.Options[(i+d+len(ff.Options))%len(ff.Options)]
	ff.val = jira.Value{Options: []jira.Option{o}}
	return m.syncCreateType()
}

// createFormIssue is the create form's issue: its own rows on the issue,
// fields the rest; the sprint when the shown one takes it.
func (m *Model) createFormIssue(f *jiraFormState, fields map[string]any) jiraFormCreate {
	cr := *f.create
	cr.in.Fields = maps.Clone(cr.in.Fields)
	if cr.in.Fields == nil {
		cr.in.Fields = map[string]any{}
	}
	maps.Copy(cr.in.Fields, fields)
	if !cr.form {
		return cr
	}
	for _, ff := range f.fields {
		switch ff.ID {
		case createTypeField:
			cr.in.Type = createFormType(f)
		case createSummaryField:
			cr.in.Summary = strings.TrimSpace(ff.val.Text)
		case createDescField:
			cr.in.Description = ff.val.Text
		}
	}
	cr.sprint, _ = m.createSprint()
	return cr
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
		return m.quit()
	case "esc":
		m.jiraCreateActive = false
		return m, nil
	case "enter":
		in := jira.NewIssue{Project: m.jiraTab.project, Type: m.jiraCreateType, Summary: strings.TrimSpace(m.jiraCreateInput.Value()),
			Parent: m.jiraCreateParent, Description: m.opts.templates[strings.ToLower(m.jiraCreateType)]}
		if m.jiraCreateParent != "" {
			in.Project = m.jiraCreateProject
		}
		if in.Summary == "" {
			m.status = "type a summary first"
			return m, nil
		}
		m.jiraCreateActive = false
		sprint, _ := m.createSprint()
		m.status = "creating " + in.Type + " in " + in.Project + "…"
		return m, m.createJiraIssue(jiraFormCreate{in: in, sprint: sprint}, m.jiraCreateTitle())
	}
	var cmd tea.Cmd
	m.jiraCreateInput, cmd = m.jiraCreateInput.Update(msg)
	return m, cmd
}

// createJiraIssue makes cr's issue: see createIssue.
func (m *Model) createJiraIssue(cr jiraFormCreate, title string) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(30*time.Second))
		defer cancel()
		return createIssue(ctx, c, cr, title)
	}
}

// createIssue creates cr.in, adds it to cr.sprint (0 for none) and links a
// clone to its source. A create Jira refuses is checked against the create
// screen: required fields it lacks (a Component) come back as a form titled
// title; "" asks nothing.
func createIssue(ctx context.Context, c *jira.Client, cr jiraFormCreate, title string) jiraCreatedMsg {
	key, err := c.CreateIssue(ctx, cr.in)
	if err != nil && title != "" {
		if fields, fErr := c.CreateFields(ctx, cr.in.Project, cr.in.Type); fErr == nil {
			if form := buildCreateForm(title, cr, fields); form != nil {
				form.err = err.Error()
				return jiraCreatedMsg{err: err, form: form}
			}
		}
	}
	switch {
	case err != nil:
	case cr.sprint != 0:
		if err = c.MoveToSprint(ctx, cr.sprint, key); err != nil {
			err = &jiraCreateSprintErr{err}
		}
	case cr.cloneOf != "":
		err = c.LinkClone(ctx, key, cr.cloneOf)
	}
	return jiraCreatedMsg{key: key, err: err}
}

// buildCreateForm is the form for the required fields in lacks, nil when
// it lacks none.
func buildCreateForm(title string, cr jiraFormCreate, fields []jira.CreateField) *jiraFormState {
	in := cr.in
	set := map[string]bool{"project": true, "issuetype": true, "summary": true,
		"description": in.Description != "" || len(in.DescriptionADF) > 0, "parent": in.Parent != "", "priority": in.Priority != ""}
	if len(in.Labels) > 0 {
		set["labels"] = true
	}
	for id := range in.Fields {
		set[id] = true
	}
	f := &jiraFormState{key: title, create: &cr}
	for _, cf := range fields {
		if !cf.Required || set[cf.ID] {
			continue
		}
		f.fields = append(f.fields, jiraFormField{FieldMeta: cf.FieldMeta, required: true})
	}
	if len(f.fields) == 0 {
		return nil
	}
	return f
}

// jiraCreateSprintErr is a create that worked but did not reach the sprint.
type jiraCreateSprintErr struct{ err error }

func (e *jiraCreateSprintErr) Error() string { return "not added to the sprint: " + e.err.Error() }

// handleJiraCreated opens the new issue and refetches the board; a failed
// create reopens the box with your summary.
func (m Model) handleJiraCreated(msg jiraCreatedMsg) (tea.Model, tea.Cmd) {
	if f := m.jiraForm; f != nil && f.create != nil && f.create.form && msg.form != nil {
		// The create form grows the rows Jira wants, all typed kept.
		for _, ff := range msg.form.fields {
			if !slices.ContainsFunc(f.fields, func(x jiraFormField) bool { return x.ID == ff.ID }) {
				f.fields = append(f.fields, ff)
			}
		}
		f.busy, f.err = false, msg.err.Error()
		return m, nil
	}
	if msg.form != nil && !m.modalOpen() {
		m.jiraForm = msg.form
		m.status = msg.form.key + " needs a few fields"
		return m, nil
	}
	if f := m.jiraForm; f != nil && f.create != nil {
		if msg.key == "" {
			f.busy, f.err = false, msg.err.Error() // the fix is one edit away
			return m, nil
		}
		m.jiraForm = nil
	}
	if msg.key == "" {
		m.fail("create: " + msg.err.Error())
		if !m.modalOpen() && m.jiraCreateInput.Value() != "" {
			m.jiraCreateActive = true // your summary back, to retry
			m.jiraCreateInput.Focus()
			m.fail("create: " + msg.err.Error() + " · enter retries")
		}
		return m, nil
	}
	m.jiraCreateInput.SetValue("") // made: nothing for a later failure to bring back
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
	inner := min(62, max(m.width-8, 20)) // narrower on a narrow screen
	m.jiraCreateInput.SetWidth(inner - 6)
	header := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).
		Render(m.jiraCreateTitle())
	hint := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).Render("↵ create · esc cancel")
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", m.jiraCreateInput.View(), "", hint)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).Render(body)
}

// createSprint is the sprint a new issue joins: the shown sprint view's,
// for an issue without a parent that is no epic; 0 for none.
func (m *Model) createSprint() (int, string) {
	if v, ok := m.jiraCurrentView(); ok && v.kind == jiraViewSprint && m.jiraCreateParent == "" && !strings.EqualFold(m.jiraCreateType, "epic") {
		return v.sprint, v.name
	}
	return 0, ""
}

// jiraCreateTitle is "New Bug in ABC", "New Bug in ABC → Sprint 12" (it
// joins the sprint shown), or "New Sub-task of ABC-1".
func (m *Model) jiraCreateTitle() string {
	if m.jiraCreateParent != "" {
		return "New " + m.jiraCreateType + " of " + m.jiraCreateParent
	}
	if id, name := m.createSprint(); id != 0 {
		return "New " + m.jiraCreateType + " in " + m.jiraTab.project + " → " + name
	}
	return "New " + m.jiraCreateType + " in " + m.jiraTab.project
}
