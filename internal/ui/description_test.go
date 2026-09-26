package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestEditDescriptionFile: the markdown lands in a temp file for the
// editor; a refused description says why instead.
func TestEditDescriptionFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	m := loadedJiraModel(t)
	_, cmd := m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "# Plan\n\n- one"})
	if cmd == nil {
		t.Fatal("expected the editor to run")
	}
	files, _ := filepath.Glob(filepath.Join(os.TempDir(), "laneway-ABC-1-*.md"))
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if b, _ := os.ReadFile(files[0]); string(b) != "# Plan\n\n- one\n" {
		t.Errorf("file = %q", b)
	}
	out, cmd := m.handleDescLoaded(descLoadedMsg{key: "ABC-1", err: os.ErrInvalid})
	if cmd != nil || !strings.Contains(out.(Model).status, "edit it in Jira") {
		t.Errorf("refusal status = %q", out.(Model).status)
	}
}

// TestEditDescriptionSave: a changed file is written, an unchanged one is
// not, and the file is removed either way.
func TestEditDescriptionSave(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	path := filepath.Join(t.TempDir(), "d.md")

	os.WriteFile(path, []byte("same\n"), 0o600)
	out, cmd := m.handleDescEdited(descEditedMsg{key: "ABC-1", path: path, before: "same"})
	if cmd != nil || !strings.Contains(out.(Model).status, "unchanged") {
		t.Error("unchanged should not write")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be removed")
	}

	os.WriteFile(path, []byte("**new** text\n"), 0o600)
	_, cmd = m.handleDescEdited(descEditedMsg{key: "ABC-1", path: path, before: "same"})
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || msg.field != "description" {
		t.Fatalf("%+v", msg)
	}
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"marks":[{"type":"strong"}]`) {
		t.Errorf("bodies = %q", bodies)
	}
}

func TestEditorCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code -w")
	if got := editorCommand("/tmp/x.md").Args; strings.Join(got, " ") != "code -w /tmp/x.md" {
		t.Errorf("args = %v", got)
	}
	t.Setenv("EDITOR", "")
	if got := editorCommand("/tmp/x.md").Args; got[0] != "vi" {
		t.Errorf("args = %v", got)
	}
}

// TestEditDescriptionKept: a placeholder line saves as the block it kept.
func TestEditDescriptionKept(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	path := filepath.Join(t.TempDir(), "d.md")
	os.WriteFile(path, []byte("changed\n\n<!-- keep:1 table: move or delete this line -->\n"), 0o600)
	table := json.RawMessage(`{"type":"table","content":[]}`)
	_, cmd := m.handleDescEdited(descEditedMsg{key: "ABC-1", path: path, before: "old", kept: []json.RawMessage{table}})
	cmd()
	if !strings.Contains(body, `{"type":"table","content":[]}`) || !strings.Contains(body, `"text":"changed"`) {
		t.Errorf("body = %s", body)
	}
}

// TestEditComment: only your comments are offered; the saved file replaces
// that comment.
func TestEditComment(t *testing.T) {
	var body, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			io.WriteString(w, `{"accountId":"me"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body, path = string(b), r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.jiraIssue.Comments = []jira.Comment{
		{ID: "1", AuthorID: "me", Author: "Me", Body: "mine", Raw: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"mine"}]}]}`)},
		{ID: "2", AuthorID: "bob", Author: "Bob", Body: "his"},
	}
	cmd := m.openCommentPicker()
	out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	if len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].id != "0" {
		t.Fatalf("items = %+v", m.jiraPicker.items)
	}
	_, cmd = m.applyJiraPick()
	loaded := cmd().(descLoadedMsg)
	if loaded.comment != "1" || loaded.md != "mine" {
		t.Fatalf("loaded = %+v", loaded)
	}
	file := filepath.Join(t.TempDir(), "c.md")
	os.WriteFile(file, []byte("mine, edited\n"), 0o600)
	_, cmd = m.handleDescEdited(descEditedMsg{key: "ABC-1", comment: "1", path: file, before: "mine"})
	cmd()
	if path != "/rest/api/3/issue/ABC-1/comment/1" || !strings.Contains(body, `"text":"mine, edited"`) {
		t.Errorf("PUT %s %s", path, body)
	}
}

// TestEditDocField: a multi-line field opens in the editor and saves as a
// document to that field.
func TestEditDocField(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	raw := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"old notes"}]}]}`)
	out, _ := m.handlePanelExtra(panelExtraMsg{key: "ABC-1", fields: []jiraFormField{
		{FieldMeta: jira.FieldMeta{ID: "customfield_7", Name: "Notes", Kind: jira.KindDoc}, raw: raw},
	}})
	m = out.(Model)
	m.fieldCursor, m.fieldCursorKey = len(panelFields), "ABC-1"
	_, cmd := m.handleRefKey(keyMsg(t, "enter"))
	loaded := cmd().(descLoadedMsg)
	if loaded.field != "customfield_7" || loaded.md != "old notes" {
		t.Fatalf("loaded = %+v", loaded)
	}
	file := filepath.Join(t.TempDir(), "n.md")
	os.WriteFile(file, []byte("- new\n- notes\n"), 0o600)
	_, cmd = m.handleDescEdited(descEditedMsg{key: "ABC-1", field: "customfield_7", path: file, before: "old notes"})
	cmd()
	if !strings.Contains(body, `"customfield_7":`) || !strings.Contains(body, `"bulletList"`) {
		t.Errorf("body = %s", body)
	}
}
