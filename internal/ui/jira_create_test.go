package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// openedForm runs cmd, what an entry point returns, and opens the create
// form it loads.
func openedForm(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	var out tea.Model
	switch msg := cmd().(type) {
	case jiraCreateTypesMsg:
		out, _ = m.handleJiraCreateTypes(msg)
	case jiraCloneDraftMsg:
		out, _ = m.handleJiraCloneDraft(msg)
	default:
		t.Fatalf("not a create form: %T", msg)
	}
	m = out.(Model)
	if m.jiraForm == nil || m.jiraForm.create == nil || !m.jiraForm.create.form {
		t.Fatalf("no create form: %q", m.status)
	}
	m.jiraForm.editing = false
	return m
}

// submitCreate types summary into the form and creates the issue.
func submitCreate(t *testing.T, m Model, summary string) jiraCreatedMsg {
	t.Helper()
	f := m.jiraForm
	f.fields[1].val, f.fields[1].changed = jira.Value{Text: summary}, true
	cmd := m.submitJiraForm()
	if cmd == nil {
		t.Fatalf("no create: %q", f.err)
	}
	return cmd().(jiraCreatedMsg)
}

// TestJiraCreate: n opens one form with the type (Task), summary and
// description; → changes the type, enter on the summary creates the issue in
// the board's project and the shown sprint, and the panel opens on it.
func TestJiraCreate(t *testing.T) {
	var created map[string]any
	var sprintBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/3/issue/createmeta/ABC/issuetypes":
			_, _ = w.Write([]byte(`{"issueTypes":[{"id":"1","name":"Task"},{"id":"2","name":"Bug"},{"id":"3","name":"Sub-task","subtask":true}]}`))
		case "POST /rest/api/3/issue":
			var body struct{ Fields map[string]any }
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = body.Fields
			_, _ = w.Write([]byte(`{"key":"ABC-9"}`))
		case "POST /rest/agile/1.0/sprint/9/issue":
			_ = json.NewDecoder(r.Body).Decode(&sprintBody)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.opts.templates = map[string]string{"bug": "## Steps"}
	out, cmd := m.handleKey(keyMsg(t, "n"))
	out, _ = out.(Model).handleJiraCreateTypes(cmd().(jiraCreateTypesMsg))
	m = out.(Model)
	f := m.jiraForm
	if f == nil || f.create == nil || !f.create.form || f.idx != 1 || !f.editing {
		t.Fatalf("form = %+v", f)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"New Task in ABC", "Type *", "Task", "Summary *", "Description"} {
		if !strings.Contains(view, want) {
			t.Errorf("form lacks %q:\n%s", want, view)
		}
	}
	if opts := f.fields[0].Options; len(opts) != 2 {
		t.Errorf("types = %+v, want no subtask", opts)
	}
	for _, k := range []string{"shift+tab", "right", "down", "enter", "C", "r", "a", "s", "h"} {
		out, _ = m.handleKey(keyStr(k))
		m = out.(Model)
	}
	if got := createFormType(m.jiraForm); got != "Bug" || !strings.HasPrefix(m.jiraForm.key, "New Bug in ABC") ||
		m.jiraForm.fields[2].val.Text != "## Steps" {
		t.Fatalf("type %q, title %q, description %q", got, m.jiraForm.key, m.jiraForm.fields[2].val.Text)
	}
	out, cmd = m.handleKey(keyStr("enter"))
	m = out.(Model)
	msg := cmd().(jiraCreatedMsg)
	if msg.key != "ABC-9" || msg.err != nil {
		t.Fatalf("created = %+v", msg)
	}
	if created["summary"] != "Crash" || created["issuetype"].(map[string]any)["name"] != "Bug" ||
		created["project"].(map[string]any)["key"] != "ABC" || created["description"] == nil {
		t.Errorf("fields = %v", created)
	}
	if len(sprintBody["issues"]) != 1 || sprintBody["issues"][0] != "ABC-9" {
		t.Errorf("sprint body = %v", sprintBody)
	}
	out, _ = m.handleJiraCreated(msg)
	m = out.(Model)
	if r := m.currentRef(); r == nil || r.jiraKey != "ABC-9" || m.status != "created ABC-9" || m.jiraForm != nil {
		t.Errorf("panel %+v, status %q, form %v", r, m.status, m.jiraForm)
	}
	if m.lastCreateType["ABC"] != "Bug" {
		t.Errorf("last type = %q", m.lastCreateType["ABC"])
	}
}

// TestJiraCreateFormEsc: esc with a summary typed asks once; a failed
// create keeps the form, grown by the fields Jira wants.
func TestJiraCreateFormEsc(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{ID: "1", Name: "Task"}}})
	m = out.(Model)
	m.jiraForm.editing = false
	m.jiraForm.fields[1].val.Text, m.jiraForm.fields[1].changed = "Crash", true
	out, _ = m.handleKey(keyStr("esc"))
	if m = out.(Model); m.jiraForm == nil || !strings.Contains(m.jiraForm.err, "esc again") {
		t.Fatal("the first esc should ask")
	}
	f := m.jiraForm
	out, _ = m.handleJiraCreated(jiraCreatedMsg{err: errors.New("Component/s is required."), form: &jiraFormState{fields: []jiraFormField{
		{FieldMeta: jira.FieldMeta{ID: "components", Name: "Components"}, required: true}}}})
	if m = out.(Model); m.jiraForm != f || len(f.fields) != 4 || f.fields[1].val.Text != "Crash" || f.err == "" {
		t.Fatalf("form after refusal = %+v", m.jiraForm)
	}
	out, _ = m.handleKey(keyStr("esc"))
	if m = out.(Model); m.jiraForm != nil {
		t.Error("the second esc should drop the form")
	}
}

// TestJiraCreateAsksRequired: a create refused for a required Component
// adds a row for it to the form, all typed kept; the pick goes with the retry.
func TestJiraCreateAsksRequired(t *testing.T) {
	var created []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/3/issue/createmeta/ABC/issuetypes":
			_, _ = w.Write([]byte(`{"issueTypes":[{"id":"2","name":"Bug"}]}`))
		case "GET /rest/api/3/issue/createmeta/ABC/issuetypes/2":
			_, _ = w.Write([]byte(`{"fields":[
				{"fieldId":"summary","name":"Summary","required":true,"schema":{"type":"string"}},
				{"fieldId":"components","name":"Components","required":true,"schema":{"type":"array","items":"component"},"allowedValues":[{"id":"10","name":"Web"},{"id":"11","name":"App"}]}]}`))
		case "POST /rest/api/3/issue":
			var body struct{ Fields map[string]any }
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body.Fields)
			if body.Fields["components"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":{"components":"Component/s is required."}}`))
				return
			}
			_, _ = w.Write([]byte(`{"key":"ABC-9"}`))
		case "POST /rest/agile/1.0/sprint/9/issue":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.openCreateForm(createSpec{in: jira.NewIssue{Project: "ABC"}, types: []string{"Bug"}})
	m.jiraForm.editing = false
	out, _ := m.handleJiraCreated(submitCreate(t, m, "Crash"))
	m = out.(Model)
	f := m.jiraForm
	if f == nil || len(f.fields) != 4 || f.fields[3].ID != "components" || f.fields[1].val.Text != "Crash" {
		t.Fatalf("form = %+v", f)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Components *") || !strings.Contains(view, "Component/s is required") || !strings.Contains(view, "[ Create ]") {
		t.Errorf("form view:\n%s", view)
	}

	f.idx = 3
	out, _ = m.handleKey(keyStr("enter")) // the Components picker
	out, _ = out.(Model).handleKey(keyStr("down"))
	out, _ = out.(Model).handleKey(keyStr("enter"))
	m = out.(Model)
	if got := jiraValueText(m.jiraForm.fields[3].val); got != "App" {
		t.Fatalf("picked %q", got)
	}
	out, cmd := m.handleKey(keyStr("ctrl+s"))
	m = out.(Model)
	if !m.jiraForm.busy {
		t.Fatal("ctrl+s sent nothing")
	}
	out, _ = m.handleJiraCreated(cmd().(jiraCreatedMsg))
	m = out.(Model)
	if m.jiraForm != nil || m.status != "created ABC-9" {
		t.Errorf("form %v, status %q", m.jiraForm, m.status)
	}
	if len(created) != 2 || created[1]["summary"] != "Crash" {
		t.Fatalf("creates = %v", created)
	}
	if c, _ := created[1]["components"].([]any); len(c) != 1 || c[0].(map[string]any)["id"] != "11" {
		t.Errorf("components = %v", created[1]["components"])
	}
}

// TestCreateFormPicksPeople: a required person field on the create form
// lists the people assignable in the project.
func TestCreateFormPicksPeople(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/user/assignable/search":
			if r.URL.Query().Get("project") != "ABC" {
				t.Errorf("query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Ada"}]`))
		case "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"me","displayName":"Me"}`))
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.jiraForm = buildCreateForm("New Bug in ABC", jiraFormCreate{in: jira.NewIssue{Project: "ABC", Type: "Bug", Summary: "Crash"}},
		[]jira.CreateField{{FieldMeta: jira.FieldMeta{ID: "customfield_1", Name: "Reviewer", Kind: jira.KindUser}, Required: true}})
	out, cmd := m.handleKey(keyStr("enter"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickFormUser {
		t.Fatal("enter opened no people picker")
	}
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if !slices.ContainsFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.label == "Ada" }) {
		t.Errorf("items = %+v", m.jiraPicker.items)
	}
}

// TestCloneAsksRequired: a clone opens the create form with its copy; a
// refusal for a required custom field adds its row; the retry keeps the
// copied components and links the clone.
func TestCloneAsksRequired(t *testing.T) {
	var created []map[string]any
	var linked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /rest/api/3/issue/ABC-1":
			_, _ = w.Write([]byte(`{"fields":{"issuetype":{"name":"Bug"},"summary":"Pay","components":[{"id":"10"}]}}`))
		case "GET /rest/api/3/issue/createmeta/ABC/issuetypes":
			_, _ = w.Write([]byte(`{"issueTypes":[{"id":"2","name":"Bug"}]}`))
		case "GET /rest/api/3/issue/createmeta/ABC/issuetypes/2":
			_, _ = w.Write([]byte(`{"fields":[
				{"fieldId":"components","name":"Components","required":true,"schema":{"type":"array","items":"component"},"allowedValues":[{"id":"10","name":"Web"}]},
				{"fieldId":"customfield_7","name":"Team","required":true,"schema":{"type":"string"}}]}`))
		case "POST /rest/api/3/issue":
			var body struct{ Fields map[string]any }
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body.Fields)
			if body.Fields["customfield_7"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":{"customfield_7":"Team is required."}}`))
				return
			}
			_, _ = w.Write([]byte(`{"key":"ABC-9"}`))
		case "GET /rest/api/3/issueLinkType":
			_, _ = w.Write([]byte(`{"issueLinkTypes":[{"name":"Cloners"}]}`))
		case "POST /rest/api/3/issueLink":
			linked = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m = openedForm(t, m, m.applyIssueAction("ABC-1", "clone"))
	f := m.jiraForm
	if f.key != "Clone of ABC-1 · Bug" || f.fields[1].val.Text != "CLONE - Pay" {
		t.Fatalf("form %q, summary %q", f.key, f.fields[1].val.Text)
	}
	out, _ := m.handleJiraCreated(submitCreate(t, m, "CLONE - Pay"))
	m = out.(Model)
	i := slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == "customfield_7" })
	if m.jiraForm != f || i < 0 {
		t.Fatalf("form = %+v", f)
	}
	f.fields[i].val, f.fields[i].changed = jira.Value{Text: "Core"}, true
	out, cmd := m.handleKey(keyStr("ctrl+s"))
	out, _ = out.(Model).handleJiraCreated(cmd().(jiraCreatedMsg))
	m = out.(Model)
	if m.jiraForm != nil || m.status != "created ABC-9" || !linked {
		t.Errorf("form %v, status %q, linked %v", m.jiraForm, m.status, linked)
	}
	if len(created) != 2 || created[1]["customfield_7"] != "Core" || created[1]["components"] == nil {
		t.Errorf("creates = %v", created)
	}

	m.jiraForm, f.busy = f, false
	out, _ = m.handleKey(keyStr("esc"))
	out, _ = out.(Model).handleKey(keyStr("esc"))
	if m = out.(Model); m.jiraForm != nil || m.status != "create cancelled" {
		t.Errorf("esc twice on a clone: form %v, %q", m.jiraForm != nil, m.status)
	}
}

// TestCreateFormRequiredFields: the type's required fields join the form as
// it opens; a type change swaps them, what was typed comes back with the
// type, and the second visit needs no fetch. A late fetch is dropped.
func TestCreateFormRequiredFields(t *testing.T) {
	m := jiraTabModel(t)
	out, cmd := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}, {Name: "Bug"}}})
	m = out.(Model)
	f := m.jiraForm
	if !f.create.loading || !strings.Contains(ansi.Strip(m.View().Content), "Task's fields loading…") {
		t.Fatal("the form should say its fields are loading")
	}
	_ = cmd
	comp := jira.CreateField{FieldMeta: jira.FieldMeta{ID: "components", Name: "Components", Kind: jira.KindOptions}, Required: true}
	optional := jira.CreateField{FieldMeta: jira.FieldMeta{ID: "labels", Name: "Labels"}}
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Task", seq: f.create.fieldsSeq, fields: []jira.CreateField{comp, optional}})
	m = out.(Model)
	if len(f.fields) != 4 || f.fields[3].ID != "components" || !f.fields[3].required || f.create.loading {
		t.Fatalf("rows = %+v", f.fields)
	}
	f.fields[3].val, f.fields[3].changed = jira.Value{Options: []jira.Option{{ID: "10", Name: "Web"}}}, true

	f.idx, f.editing = 0, false
	late := f.create.fieldsSeq
	out, _ = m.handleKey(keyStr("right")) // Bug: fetched
	m = out.(Model)
	if createFormType(f) != "Bug" || !f.create.loading {
		t.Fatalf("type %q, loading %v", createFormType(f), f.create.loading)
	}
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Task", seq: late, fields: []jira.CreateField{comp}})
	if m = out.(Model); !f.create.loading {
		t.Error("a stale fetch should be dropped")
	}
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Bug", seq: f.create.fieldsSeq})
	if m = out.(Model); len(f.fields) != 3 {
		t.Fatalf("Bug rows = %+v", f.fields)
	}
	out, cmd = m.handleKey(keyStr("left")) // Task again: cached
	m = out.(Model)
	if cmd != nil || len(f.fields) != 4 || jiraValueText(f.fields[3].val) != "Web" {
		t.Errorf("Task again: cmd %v, rows %+v", cmd != nil, f.fields)
	}
}

// TestCreateFormErrors: the hint names the empty required rows; Jira's
// reasons land under the fields they name until the value changes, the rest
// on top, and all typed stays.
func TestCreateFormErrors(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
	m = out.(Model)
	f := m.jiraForm
	f.editing = false
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "fill in Summary to create") {
		t.Errorf("hint should name the summary:\n%s", view)
	}
	f.fields[1].val, f.fields[1].changed = jira.Value{Text: "Crash"}, true
	refused := &jira.RequestError{Messages: []string{"Workflow closed"}, Fields: map[string]string{"summary": "Too short.", "customfield_9": "Team is required."}}
	out, _ = m.handleJiraCreated(jiraCreatedMsg{err: refused})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Too short.") || !strings.Contains(view, "Workflow closed; customfield_9: Team is required.") ||
		strings.Index(view, "Workflow closed") > strings.Index(view, "Summary") {
		t.Errorf("errors:\n%s", view)
	}
	if f.fields[1].val.Text != "Crash" || f.busy {
		t.Error("the typed summary should stay")
	}
	f.fields[1].val.Text = "Crash on save"
	if strings.Contains(ansi.Strip(m.View().Content), "Too short.") {
		t.Error("an edited field drops Jira's message")
	}
}

// TestCreateFormMoreFields: the form starts minimal; the toggle row shows
// every field it can edit, typed values survive hiding them, a picked one
// goes with the create, and the choice holds for the next form.
func TestCreateFormMoreFields(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
	m = out.(Model)
	f := m.jiraForm
	f.editing = false
	screen := []jira.CreateField{
		{FieldMeta: jira.FieldMeta{ID: "priority", Name: "Priority", Kind: jira.KindOption, Options: []jira.Option{{ID: "2", Name: "High"}}}},
		{FieldMeta: jira.FieldMeta{ID: "labels", Name: "Labels", Kind: jira.KindText}},
		{FieldMeta: jira.FieldMeta{ID: "attachment", Name: "Attachment", Kind: jira.KindOther}},
	}
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Task", seq: f.create.fieldsSeq, fields: screen})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "+ 2 more fields") || strings.Contains(view, "Priority") {
		t.Fatalf("minimal form:\n%s", view)
	}
	f.idx = 3
	out, _ = m.handleKey(keyStr("enter"))
	m = out.(Model)
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "− fewer fields") || !strings.Contains(view, "Priority") || strings.Contains(view, "Attachment") {
		t.Fatalf("more fields:\n%s", view)
	}
	f.fields[4].val, f.fields[4].changed = jira.Value{Options: []jira.Option{{ID: "2", Name: "High"}}}, true
	m.toggleCreateMore()
	m.toggleCreateMore()
	if jiraValueText(f.fields[4].val) != "High" {
		t.Fatal("a typed value should survive hiding")
	}
	f.fields[1].val, f.fields[1].changed = jira.Value{Text: "Crash"}, true
	cr := m.createFormIssue(f, map[string]any{"priority": map[string]string{"id": "2"}})
	if cr.in.Fields["priority"] == nil || cr.in.Fields[createMoreField] != nil {
		t.Errorf("fields = %v", cr.in.Fields)
	}
	if !m.createMore {
		t.Error("the choice should hold for the next form")
	}
}

// TestCreateFormSprintParent: among the more fields the sprint starts on
// the shown one and can go to the backlog, and a parent can be set unless
// the form's parent is fixed.
func TestCreateFormSprintParent(t *testing.T) {
	m := jiraTabModel(t)
	m.createMore = true
	out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
	m = out.(Model)
	f := m.jiraForm
	f.editing = false
	screen := []jira.CreateField{
		{FieldMeta: jira.FieldMeta{ID: "parent", Name: "Parent", Kind: jira.KindIssue}},
		{FieldMeta: jira.FieldMeta{ID: "customfield_10020", Name: "Sprint", Kind: jira.KindSprint}},
	}
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Task", seq: f.create.fieldsSeq, fields: screen})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Parent") || !strings.Contains(view, "Sprint 1") {
		t.Fatalf("form:\n%s", view)
	}
	i := slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.Kind == jira.KindSprint })
	if cr := m.createFormIssue(f, nil); cr.sprint != 9 {
		t.Errorf("sprint %d, want the shown 9", cr.sprint)
	}
	f.fields[i].val, f.fields[i].changed = jira.Value{Options: []jira.Option{{ID: "", Name: "none (backlog)"}}}, true
	if cr := m.createFormIssue(f, nil); cr.sprint != 0 || strings.Contains(m.jiraCreateTitle(), "→") {
		t.Errorf("backlog: sprint %d, title %q", cr.sprint, m.jiraCreateTitle())
	}

	m.jiraForm = nil
	out, _ = m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", parent: "ABC-1", types: []jira.Option{{Name: "Sub-task"}}})
	m = out.(Model)
	f = m.jiraForm
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Sub-task", seq: f.create.fieldsSeq, fields: screen})
	m = out.(Model)
	if slices.ContainsFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == "parent" }) {
		t.Error("a subtask's parent is fixed, no row")
	}
}

// TestCreateFormMention: @ in the description completes people of the
// project, and the create sends them as mentions.
func TestCreateFormMention(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
	m = out.(Model)
	f := m.jiraForm
	f.editing = false
	f.idx = slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == createDescField })
	m.editJiraFormField()
	var cmd tea.Cmd
	for _, k := range []string{"h", "i", " ", "@", "a", "d"} {
		out, cmd = m.handleJiraFormKey(keyStr(k))
		m = out.(Model)
	}
	if cmd == nil {
		t.Fatal("@ad should schedule a search")
	}
	if ed, key := m.mentionEditor(); ed == nil || key != "ABC" {
		t.Fatalf("mention editor %v in %q", ed, key)
	}
	out, _ = m.Update(mentionFoundMsg{seq: m.jiraMention.seq, users: []jira.User{{AccountID: "a1", DisplayName: "Ada"}}})
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "▸ @Ada") || !strings.Contains(view, "@ mention") {
		t.Fatalf("no completion:\n%s", view)
	}
	out, _ = m.handleJiraFormKey(keyMsg(t, "tab"))
	m = out.(Model)
	out, _ = m.handleJiraFormKey(keyMsg(t, "ctrl+s"))
	m = out.(Model)
	if v := f.fields[f.idx].val.Text; v != "hi @Ada " {
		t.Fatalf("description %q", v)
	}
	if cr := m.createFormIssue(f, nil); len(cr.in.Mentions) != 1 || cr.in.Mentions[0].AccountID != "a1" {
		t.Errorf("mentions %v", cr.in.Mentions)
	}
}
