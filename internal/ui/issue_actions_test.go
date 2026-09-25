package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestUploadAction: A → upload asks a path; a missing file is refused, a
// real one is posted to the issue.
func TestUploadAction(t *testing.T) {
	m, writes := actionsModel(t, nil)
	m, _ = pickAction(t, m, "upload")
	if !m.jiraFieldActive || m.jiraFieldName != "upload" || m.jiraFieldKey != "ABC-1" {
		t.Fatalf("input: %q %q", m.jiraFieldName, m.jiraFieldKey)
	}
	m.jiraFieldInput.SetValue("/no/such/file")
	out, cmd := m.applyJiraField()
	if cmd != nil || !strings.Contains(out.(Model).status, "no such file") {
		t.Error("a missing file should be refused")
	}
	path := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(path, []byte("x"), 0o600)
	m.jiraFieldInput.SetValue(path)
	_, cmd = m.applyJiraField()
	if msg := cmd().(jiraMutatedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if w := writes(); len(w) != 1 || !strings.HasPrefix(w[0], "POST /rest/api/3/issue/ABC-1/attachments") || !strings.Contains(w[0], "notes.txt") {
		t.Errorf("writes = %q", w)
	}
}

// TestDownloadAction: with attachments, A offers download; the pick lands
// in the download dir.
func TestDownloadAction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dir)
	m, _ := actionsModel(t, map[string]string{"/rest/api/3/attachment/content/7": "DATA"})
	m.jiraIssue.Attachments = []jira.Attachment{{ID: "7", Filename: "trace.log", Size: 4}}
	m, _ = pickAction(t, m, "download")
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickAttachment || len(m.jiraPicker.items) != 1 {
		t.Fatalf("picker = %+v", m.jiraPicker)
	}
	_, cmd := m.applyJiraPick()
	msg := cmd().(jiraDownloadedMsg)
	if msg.err != nil || msg.path != filepath.Join(dir, "trace.log") {
		t.Fatalf("%+v", msg)
	}
	if b, _ := os.ReadFile(msg.path); string(b) != "DATA" {
		t.Errorf("content %q", b)
	}
}

// TestUnlinkAction: A → remove a link lists the issue links, not the
// parent; the pick deletes that link.
func TestUnlinkAction(t *testing.T) {
	m, writes := actionsModel(t, nil)
	m.jiraIssue.Links = []jira.Link{{Rel: "parent", Key: "ABC-5"}, {Rel: "blocks", Key: "ABC-7", Summary: "Seven", LinkID: "10200"}}
	m, _ = pickAction(t, m, "unlink")
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickUnlink || len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].label != "blocks ABC-7 Seven" {
		t.Fatalf("picker = %+v", m.jiraPicker.items)
	}
	_, cmd := m.applyJiraPick()
	cmd()
	if w := writes(); len(w) != 1 || !strings.HasPrefix(w[0], "DELETE /rest/api/3/issueLink/10200") {
		t.Errorf("writes = %q", w)
	}
}

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "report-a.txt"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "report-b.txt"), nil, 0o600)
	os.Mkdir(filepath.Join(dir, "shots"), 0o700)
	os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o600)
	if got, names := completePath(dir + "/rep"); got != dir+"/report-" || len(names) != 2 {
		t.Errorf("rep → %q %v", got, names)
	}
	if got, _ := completePath(dir + "/sh"); got != dir+"/shots/" {
		t.Errorf("sh → %q", got)
	}
	if _, names := completePath(dir + "/"); len(names) != 3 {
		t.Errorf("hidden files should be left out: %v", names)
	}
	if got, names := completePath(dir + "/zzz"); got != dir+"/zzz" || names != nil {
		t.Errorf("no match → %q %v", got, names)
	}
}
