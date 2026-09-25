package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// actionsModel is the panel on ABC-1 with a fake Jira behind it.
func actionsModel(t *testing.T, gets map[string]string) (Model, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, gets[r.URL.Path])
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		if r.URL.Path == "/rest/api/3/issue" {
			io.WriteString(w, `{"key":"ABC-9"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	return m, func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(writes) }
}

// pickAction opens A and picks id, loading what the pick fetches.
func pickAction(t *testing.T, m Model, id string) (Model, tea.Cmd) {
	t.Helper()
	out, _ := m.handleRefKey(keyMsg(t, "A"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickIssueActions {
		t.Fatal("A should open the actions")
	}
	m.jiraPicker.idx = slices.IndexFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == id })
	out, cmd := m.applyJiraPick()
	return out.(Model), cmd
}

// TestSubtask: a subtask type, a summary, then created under ABC-1 in its
// project and not moved to a sprint.
func TestSubtask(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{
		"/rest/api/3/issue/createmeta/ABC/issuetypes": `{"issueTypes":[{"id":"1","name":"Story"},{"id":"5","name":"Sub-task","subtask":true}]}`,
	})
	m, cmd := pickAction(t, m, "subtask")
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].id != "Sub-task" {
		t.Fatalf("types = %+v", m.jiraPicker.items)
	}
	out, _ = m.applyJiraPick()
	m = out.(Model)
	if !strings.Contains(m.View().Content, "New Sub-task of ABC-1") {
		t.Error("summary modal title")
	}
	m.jiraCreateInput.SetValue("Write tests")
	_, cmd = m.handleJiraCreateKey(keyMsg(t, "enter"))
	if msg := cmd().(jiraCreatedMsg); msg.err != nil || msg.key != "ABC-9" {
		t.Fatalf("%+v", msg)
	}
	w := writes()
	if len(w) != 1 || !strings.Contains(w[0], `"parent":{"key":"ABC-1"}`) || !strings.Contains(w[0], `"issuetype":{"name":"Sub-task"}`) {
		t.Errorf("writes = %q", w)
	}
}

// TestLinkAction: a direction, a bare number, and the link as Jira reads it.
func TestLinkAction(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{
		"/rest/api/3/issueLinkType": `{"issueLinkTypes":[{"name":"Blocks","inward":"is blocked by","outward":"blocks"},{"name":"Relates","inward":"relates to","outward":"relates to"}]}`,
	})
	m, cmd := pickAction(t, m, "link")
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	var labels []string
	for _, it := range m.jiraPicker.items {
		labels = append(labels, it.label)
	}
	if !slices.Equal(labels, []string{"ABC-1 blocks …", "ABC-1 is blocked by …", "ABC-1 relates to …"}) {
		t.Fatalf("labels = %q", labels)
	}
	m.jiraPicker.idx = 1 // is blocked by
	out, _ = m.applyJiraPick()
	m = out.(Model)
	m.jiraFieldInput.SetValue("7")
	_, cmd = m.applyJiraField()
	cmd()
	w := writes()
	if len(w) != 1 || !strings.Contains(w[0], `"outwardIssue":{"key":"ABC-7"}`) || !strings.Contains(w[0], `"inwardIssue":{"key":"ABC-1"}`) {
		t.Errorf("writes = %q", w)
	}
}
