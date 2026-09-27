package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestJiraCreate: n picks a type, the summary creates the issue in the
// board's project and the shown sprint, and the panel opens on it.
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
	out, cmd := m.handleKey(keyMsg(t, "n"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickCreateType {
		t.Fatal("n opened no type picker")
	}
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if len(m.jiraPicker.items) != 2 {
		t.Fatalf("types = %+v, want no subtask", m.jiraPicker.items)
	}
	out, _ = m.handleKey(keyMsg(t, "down"))
	out, _ = out.(Model).handleKey(keyStr("enter"))
	m = out.(Model)
	if !m.jiraCreateActive || m.jiraCreateType != "Bug" {
		t.Fatalf("summary prompt = %v %q", m.jiraCreateActive, m.jiraCreateType)
	}
	for _, r := range "Crash" {
		out, _ = m.handleKey(keyStr(string(r)))
		m = out.(Model)
	}
	out, cmd = m.handleKey(keyStr("enter"))
	m = out.(Model)
	msg := cmd().(jiraCreatedMsg)
	if msg.key != "ABC-9" || msg.err != nil {
		t.Fatalf("created = %+v", msg)
	}
	if created["summary"] != "Crash" || created["issuetype"].(map[string]any)["name"] != "Bug" ||
		created["project"].(map[string]any)["key"] != "ABC" {
		t.Errorf("fields = %v", created)
	}
	if len(sprintBody["issues"]) != 1 || sprintBody["issues"][0] != "ABC-9" {
		t.Errorf("sprint body = %v", sprintBody)
	}
	out, _ = m.handleJiraCreated(msg)
	m = out.(Model)
	if r := m.currentRef(); r == nil || r.jiraKey != "ABC-9" || m.status != "created ABC-9" {
		t.Errorf("panel %+v, status %q", r, m.status)
	}
}

// TestJiraCreateAsksRequired: a create refused for a required Component
// opens the form for it; the pick goes with the retry, esc goes back.
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
	m.openJiraCreateSummary("Bug")
	m.jiraCreateInput.SetValue("Crash")
	out, cmd := m.handleKey(keyStr("enter"))
	out, _ = out.(Model).handleJiraCreated(cmd().(jiraCreatedMsg))
	m = out.(Model)
	f := m.jiraForm
	if f == nil || f.create == nil || len(f.fields) != 1 || f.fields[0].ID != "components" {
		t.Fatalf("form = %+v", f)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Components *") || !strings.Contains(view, "Component/s is required") || !strings.Contains(view, "[ Create ]") {
		t.Errorf("form view:\n%s", view)
	}

	out, _ = m.handleKey(keyStr("esc"))
	m = out.(Model)
	if m.jiraForm != nil || !m.jiraCreateActive || m.jiraCreateInput.Value() != "Crash" {
		t.Fatalf("esc: form %v, box %v %q", m.jiraForm, m.jiraCreateActive, m.jiraCreateInput.Value())
	}
	m.jiraForm, m.jiraCreateActive = f, false

	out, _ = m.handleKey(keyStr("enter")) // the Components picker
	out, _ = out.(Model).handleKey(keyStr("down"))
	out, _ = out.(Model).handleKey(keyStr("enter"))
	m = out.(Model)
	if got := jiraValueText(m.jiraForm.fields[0].val); got != "App" {
		t.Fatalf("picked %q", got)
	}
	out, cmd = m.handleKey(keyStr("ctrl+s"))
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
	m.jiraForm = buildCreateForm("New Bug in ABC", jira.NewIssue{Project: "ABC", Type: "Bug", Summary: "Crash"}, 0,
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
