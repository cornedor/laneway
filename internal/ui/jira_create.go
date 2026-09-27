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
	key  string
	err  error
	form *jiraFormState // the fields a failed create lacks, to fill in
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
