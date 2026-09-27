package ui

import (
	"errors"
	"github.com/charmbracelet/x/ansi"
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

	"github.com/cornedor/laneway/internal/config"
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
	// ABC-1 is blocked by ABC-7: ABC-7 blocks, so it goes in inwardIssue.
	if len(w) != 1 || !strings.Contains(w[0], `"inwardIssue":{"key":"ABC-7"}`) || !strings.Contains(w[0], `"outwardIssue":{"key":"ABC-1"}`) {
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

// TestPasteAction: A → paste uploads the clipboard's PNG; no image says so.
func TestPasteAction(t *testing.T) {
	m, writes := actionsModel(t, nil)
	orig := clipboardImage
	t.Cleanup(func() { clipboardImage = orig })
	clipboardImage = func([]string) ([]byte, error) { return []byte("\x89PNG-data"), nil }
	_, cmd := pickAction(t, m, "paste")
	if msg := cmd().(jiraMutatedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if w := writes(); len(w) != 1 || !strings.HasPrefix(w[0], "POST /rest/api/3/issue/ABC-1/attachments") ||
		!strings.Contains(w[0], `filename="pasted-`) || !strings.Contains(w[0], "PNG-data") {
		t.Errorf("writes = %q", w)
	}
	clipboardImage = func([]string) ([]byte, error) { return nil, errors.New("no image on the clipboard") }
	_, cmd = pickAction(t, m, "paste")
	if msg := cmd().(jiraMutatedMsg); msg.err == nil || len(writes()) != 1 {
		t.Error("no image should upload nothing")
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
	out, cmd := m.applyJiraPick()
	if m = out.(Model); cmd != nil || !m.jiraPicker.active || !strings.Contains(m.status, "enter again removes") {
		t.Fatalf("first enter: %q", m.status)
	}
	_, cmd = m.applyJiraPick()
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

// TestCreateTemplate: a new issue of a type with a template starts with it.
func TestCreateTemplate(t *testing.T) {
	m, writes := actionsModel(t, nil)
	m.opts.templates = map[string]string{"bug": "## Steps\n\n1. "}
	m.jiraTab.project = "ABC"
	m.openJiraCreateSummary("Bug")
	m.jiraCreateInput.SetValue("Cart breaks")
	_, cmd := m.handleJiraCreateKey(keyMsg(t, "enter"))
	cmd()
	if w := writes(); len(w) != 1 || !strings.Contains(w[0], `"type":"heading"`) || !strings.Contains(w[0], `"text":"Steps"`) {
		t.Errorf("writes = %q", w)
	}
}

// TestFlagAction: A → flag clears the flag of a flagged card, and cards
// show the flag.
func TestFlagAction(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{"/rest/api/3/field": `[{"id":"customfield_50","name":"Flagged"}]`})
	m.jiraTab.cards = []jira.Card{{Key: "ABC-1", Flagged: true}}
	if !strings.Contains(ansi.Strip(jiraCardLines(m.jiraTab.cards[0], true, allCardFields)[0]), "⚑") {
		t.Error("flagged card lacks ⚑")
	}
	_, cmd := pickAction(t, m, "flag")
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || msg.field != "flag cleared" {
		t.Fatalf("%+v", msg)
	}
	if w := writes(); len(w) != 1 || !strings.HasSuffix(w[0], `{"fields":{"customfield_50":null}}`) {
		t.Errorf("writes = %q", w)
	}
}

// TestFlagActionOffBoard: an issue not on the board is asked for its flag,
// so a flagged one can be cleared.
func TestFlagActionOffBoard(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{
		"/rest/api/3/field":       `[{"id":"customfield_50","name":"Flagged"}]`,
		"/rest/api/3/issue/ABC-1": `{"fields":{"customfield_50":[{"value":"Impediment"}]}}`,
	})
	m.jiraTab.cards = nil
	_, cmd := pickAction(t, m, "flag")
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || msg.field != "flag cleared" {
		t.Fatalf("%+v", msg)
	}
	if w := writes(); len(w) != 1 || !strings.HasSuffix(w[0], `{"fields":{"customfield_50":null}}`) {
		t.Errorf("writes = %q", w)
	}
}

func TestClipboardImageCommand(t *testing.T) {
	png := filepath.Join(t.TempDir(), "c.png")
	os.WriteFile(png, []byte("\x89PNG-data"), 0o600)
	if img, err := clipboardImage([]string{"cat", png}); err != nil || string(img) != "\x89PNG-data" {
		t.Errorf("img %q err %v", img, err)
	}
	if _, err := clipboardImage([]string{"no-such-tool-xyz"}); err == nil || !strings.Contains(err.Error(), "ui.clipboard_image") {
		t.Errorf("err = %v", err)
	}
	o, _ := optionsFrom(config.UIConfig{ClipboardImage: "cat /tmp/x.png", Open: "wslview"})
	if len(o.clipboardImage) != 2 || len(o.openCmd) != 1 {
		t.Errorf("commands %q %q", o.clipboardImage, o.openCmd)
	}
}

// TestChangeType: A → change type lists the other non-subtask types and
// writes the picked one's id.
func TestChangeType(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{
		"/rest/api/3/issue/createmeta/ABC/issuetypes": `{"issueTypes":[{"id":"1","name":"Task"},{"id":"2","name":"Bug"},{"id":"5","name":"Sub-task","subtask":true}]}`,
	})
	m.jiraIssue.Type = "Task"
	m, cmd := pickAction(t, m, "type")
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].value != "Bug" {
		t.Fatalf("types = %+v", m.jiraPicker.items)
	}
	_, cmd = m.applyJiraPick()
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || msg.field != "type" {
		t.Fatalf("msg = %+v", msg)
	}
	if w := writes(); len(w) != 1 || w[0] != `PUT /rest/api/3/issue/ABC-1 {"fields":{"issuetype":{"id":"2"}}}` {
		t.Errorf("writes = %q", w)
	}
}

// TestMoveToProject: A → move lists the other projects, then the target's
// types with the issue's own first; the moved issue opens under its new key.
func TestMoveToProject(t *testing.T) {
	m, _ := actionsModel(t, map[string]string{
		"/rest/api/3/project/search":                  `{"values":[{"key":"ABC","name":"Alpha"},{"key":"XYZ","name":"Xylo"}]}`,
		"/rest/api/3/issue/createmeta/ABC/issuetypes": `{"issueTypes":[{"id":"1","name":"Task"}]}`,
		"/rest/api/3/issue/createmeta/XYZ/issuetypes": `{"issueTypes":[{"id":"7","name":"Bug"},{"id":"8","name":"Task"}]}`,
	})
	m.jiraIssue.Type = "Task"
	m, cmd := pickAction(t, m, "move")
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].id != "XYZ" {
		t.Fatalf("projects = %+v", m.jiraPicker.items)
	}
	out, cmd = m.applyJiraPick()
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if it := m.jiraPicker.items[m.jiraPicker.idx]; it.id != "XYZ,8" {
		t.Fatalf("cursor on %+v, want the Task type", it)
	}
	out, cmd = m.applyJiraPick()
	if m = out.(Model); cmd == nil || !strings.Contains(m.status, "moving ABC-1 to XYZ") {
		t.Fatalf("status %q", m.status)
	}
	out, _ = m.handleJiraRelocated(jiraRelocatedMsg{key: "ABC-1", next: "XYZ-5"})
	if m = out.(Model); m.status != "moved ABC-1 to XYZ-5" || m.refs[m.refIdx].jiraKey != "XYZ-5" {
		t.Errorf("status %q, panel on %q", m.status, m.refs[m.refIdx].jiraKey)
	}
}

// TestDeleteIssue: A → delete asks a second enter, deletes the subtasks with
// it, and closes the panel.
func TestDeleteIssue(t *testing.T) {
	m, writes := actionsModel(t, nil)
	m.jiraIssue.Links = append(m.jiraIssue.Links, jira.Link{Rel: "subtask", Key: "ABC-7"})
	m, _ = pickAction(t, m, "delete")
	if it := m.jiraPicker.items; len(it) != 1 || it[0].label != "Delete ABC-1 and its subtask" {
		t.Fatalf("items = %+v", it)
	}
	out, cmd := m.applyJiraPick()
	if m = out.(Model); cmd != nil || !m.jiraPicker.active {
		t.Fatal("the first enter should only ask")
	}
	out, cmd = m.applyJiraPick()
	m = out.(Model)
	msg := cmd().(jiraDeletedMsg)
	if w := writes(); msg.err != nil || len(w) != 1 || w[0] != "DELETE /rest/api/3/issue/ABC-1 " {
		t.Fatalf("err %v, writes %q", msg.err, w)
	}
	out, _ = m.handleJiraDeleted(msg)
	if m = out.(Model); m.refOpen || m.status != "deleted ABC-1" {
		t.Errorf("panel open %v, status %q", m.refOpen, m.status)
	}
}

// TestWebLinks: the panel lists the issue's web links, L offers them, and
// A adds one.
func TestWebLinks(t *testing.T) {
	m, writes := actionsModel(t, nil)
	out, _ := m.handlePanelExtra(panelExtraMsg{key: "ABC-1", webLinks: []jira.WebLink{{Title: "Spec", URL: "https://wiki.test/spec", App: "Confluence"}}})
	m = out.(Model)
	if c := ansi.Strip(m.View().Content); !strings.Contains(c, "Web links (1)") || !strings.Contains(c, "Spec · Confluence") {
		t.Error("panel lacks the web link")
	}
	m.openJiraLinkPicker()
	if it := m.jiraPicker.items; len(it) == 0 || it[len(it)-1].id != "https://wiki.test/spec" {
		t.Errorf("L items = %+v", it)
	}
	m.closeJiraPicker()
	m, _ = pickAction(t, m, "weblink")
	m.jiraFieldInput.SetValue("https://y.test Design doc")
	out, cmd := m.applyJiraField()
	if m = out.(Model); cmd == nil {
		t.Fatalf("no write: %q", m.status)
	}
	cmd()
	if w := writes(); len(w) != 1 || w[0] != `POST /rest/api/3/issue/ABC-1/remotelink {"object":{"title":"Design doc","url":"https://y.test"}}` {
		t.Errorf("writes = %q", w)
	}
}

// TestWatchers: the watchers come first, ticked; enter on one removes them,
// on anyone else adds them.
func TestWatchers(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{
		"/rest/api/3/issue/ABC-1/watchers":  `{"watchers":[{"accountId":"a1","displayName":"Ada"}]}`,
		"/rest/api/3/user/viewissue/search": `[{"accountId":"a1","displayName":"Ada"},{"accountId":"b2","displayName":"Bob"}]`,
	})
	m, cmd := pickAction(t, m, "watchers")
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	it := m.jiraPicker.items
	if len(it) != 2 || !it[0].current || it[0].label != "Ada" || it[1].current || it[1].label != "Bob" {
		t.Fatalf("items = %+v", it)
	}
	base := m
	m.jiraPicker.idx = 1
	_, cmd = m.applyJiraPick()
	cmd()
	m = base
	m.jiraPicker.idx = 0
	_, cmd = m.applyJiraPick()
	cmd()
	if w := writes(); len(w) != 2 || w[0] != `POST /rest/api/3/issue/ABC-1/watchers "b2"` || w[1] != "DELETE /rest/api/3/issue/ABC-1/watchers " {
		t.Errorf("writes = %q", w)
	}
}

// TestReporter: the Reporter row edits like the assignee, searching the
// people who can see the issue, and writes the pick.
func TestReporter(t *testing.T) {
	m, writes := actionsModel(t, map[string]string{
		"/rest/api/3/user/viewissue/search": `[{"accountId":"a1","displayName":"Ada"},{"accountId":"b2","displayName":"Bob"}]`,
	})
	m.jiraIssue.Reporter, m.jiraIssue.ReporterAccountID = "Ada", "a1"
	cmd := panelFields[panelFieldRow("Reporter")].edit(&m)
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if it := m.jiraPicker.items; len(it) != 2 || !it[0].current || m.jiraPicker.inline != "Reporter" {
		t.Fatalf("items = %+v, inline %q", it, m.jiraPicker.inline)
	}
	m.jiraPicker.idx = 1
	_, cmd = m.applyJiraPick()
	cmd()
	if w := writes(); len(w) != 1 || w[0] != `PUT /rest/api/3/issue/ABC-1 {"fields":{"reporter":{"accountId":"b2"}}}` {
		t.Errorf("writes = %q", w)
	}
}
