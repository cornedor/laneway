package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// undoJira is a fake Jira that records the writes.
func undoJira(t *testing.T, m *Model) *[]string {
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/priority" {
			io.WriteString(w, `[{"id":"3","name":"Medium"},{"id":"1","name":"Highest"}]`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	return &writes
}

// TestUndoPanelEdit: u after a panel edit writes the value before it back.
func TestUndoPanelEdit(t *testing.T) {
	m := jiraTabModel(t)
	writes := undoJira(t, &m)
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Summary: "Old", AssigneeAccountID: "a1"}
	m.openJiraTextInput("summary", "New", "", 0)
	m.jiraFieldKey = "ABC-1"
	out, cmd := m.applyJiraField()
	m = out.(Model)
	cmd()
	out, cmd = m.handleJiraKey(keyMsg(t, "u"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("u should undo the edit")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	if last := (*writes)[len(*writes)-1]; !strings.Contains(last, `"summary":"Old"`) || m.status != "undid ABC-1 summary" {
		t.Errorf("wrote %q, status %q", last, m.status)
	}
	if _, cmd := m.handleJiraKey(keyMsg(t, "u")); cmd != nil {
		t.Error("an edit undoes once")
	}
}

// TestUndoBulk: u after a bulk priority puts each card's own back.
func TestUndoBulk(t *testing.T) {
	m := jiraTabModel(t)
	writes := undoJira(t, &m)
	m.jiraTab.cards[0].Priority, m.jiraTab.cards[1].Priority = "Medium", "Highest"
	m.applyBulkPick(jiraPickPriority, []string{"ABC-1", "ABC-2"}, jiraPickerItem{id: "2", label: "High"})
	cmd := m.undoJiraMove()
	if cmd == nil {
		t.Fatal("u should undo the bulk edit")
	}
	if msg := cmd().(editUndoneMsg); msg.err != nil || msg.what != "priority on 2 issues" {
		t.Fatalf("%+v", msg)
	}
	got := strings.Join(*writes, "\n")
	if !strings.Contains(got, `/issue/ABC-1 {"fields":{"priority":{"id":"3"}}}`) || !strings.Contains(got, `/issue/ABC-2 {"fields":{"priority":{"id":"1"}}}`) {
		t.Errorf("writes:\n%s", got)
	}
}

// TestUndoDeleteComment: u reposts a deleted comment's body.
func TestUndoDeleteComment(t *testing.T) {
	m := jiraTabModel(t)
	writes := undoJira(t, &m)
	m.undoDeleteComment("ABC-1", jira.Comment{ID: "9", Raw: []byte(`{"type":"doc","version":1,"content":[]}`)})
	if msg := m.undoJiraMove()().(editUndoneMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if last := (*writes)[len(*writes)-1]; !strings.HasPrefix(last, "POST /rest/api/3/issue/ABC-1/comment {\"body\":{\"type\":\"doc\"") {
		t.Errorf("wrote %q", last)
	}
}
