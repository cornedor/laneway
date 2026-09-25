package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
