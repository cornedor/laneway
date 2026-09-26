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

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestEditDescriptionInline: the markdown opens in the in-app editor;
// ctrl+e hands it to $EDITOR through a temp file; a refused description
// says why instead.
func TestEditDescriptionInline(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	m := loadedJiraModel(t)
	out, _ := m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "# Plan\n\n- one"})
	m = out.(Model)
	if m.descEdit == nil || m.descEdit.input.Value() != "# Plan\n\n- one" {
		t.Fatal("the editor should open on the markdown")
	}
	v := m.View()
	if c := ansi.Strip(v.Content); strings.Contains(c, "Description — ABC-1") || !strings.Contains(c, "Description  ctrl+s save") || !strings.Contains(c, "┃ # Plan") {
		t.Fatalf("the editor should sit in the panel under the Description head:\n%s", c)
	}
	if v.Cursor == nil {
		t.Error("the terminal cursor should sit in the editor")
	}
	out, _ = m.Update(keyMsg(t, "!"))
	if m = out.(Model); !strings.Contains(ansi.Strip(m.View().Content), "- one!") {
		t.Error("typing should redraw the editor in the panel")
	}
	m.descEdit.input.SetValue("# Plan\n\n- one")
	out, cmd := m.handleKey(keyMsg(t, "ctrl+e"))
	if m = out.(Model); cmd == nil || m.descEdit != nil {
		t.Fatal("ctrl+e should run $EDITOR")
	}
	files, _ := filepath.Glob(filepath.Join(os.TempDir(), "laneway-ABC-1-*.md"))
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if b, _ := os.ReadFile(files[0]); string(b) != "# Plan\n\n- one\n" {
		t.Errorf("file = %q", b)
	}
	out, cmd = m.handleDescLoaded(descLoadedMsg{key: "ABC-1", err: os.ErrInvalid})
	if cmd != nil || !strings.Contains(out.(Model).status, "edit it in Jira") {
		t.Errorf("refusal status = %q", out.(Model).status)
	}
}

// TestEditDescriptionInlineSave: enter is a newline, ctrl+s saves; esc on
// changed text asks once before discarding.
func TestEditDescriptionInlineSave(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, _ := m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "old"})
	m = out.(Model)
	for _, k := range []string{"enter", "n", "e", "w"} {
		out, _ = m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	if v := m.descEdit.input.Value(); v != "old\nnew" {
		t.Fatalf("value = %q", v)
	}
	out, _ = m.handleKey(keyMsg(t, "esc"))
	if m = out.(Model); m.descEdit == nil {
		t.Fatal("a first esc on changed text should ask")
	}
	out, cmd := m.handleKey(keyMsg(t, "ctrl+s"))
	if m = out.(Model); cmd == nil || m.descEdit != nil {
		t.Fatal("ctrl+s should save")
	}
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || len(bodies) != 1 || !strings.Contains(bodies[0], `"text":"new"`) {
		t.Errorf("err %v bodies %q", msg.err, bodies)
	}
	out, _ = m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "same"})
	m = out.(Model)
	out, _ = m.handleKey(keyMsg(t, "esc"))
	if m = out.(Model); m.descEdit != nil {
		t.Error("esc on unchanged text should close at once")
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

// TestEditKeptOnFailure: a save Jira rejects keeps the file and says where.
func TestEditKeptOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["nope"]}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	path := filepath.Join(t.TempDir(), "d.md")
	os.WriteFile(path, []byte("hours of writing\n"), 0o600)
	_, cmd := m.handleDescEdited(descEditedMsg{key: "ABC-1", path: path, before: "old"})
	msg := cmd().(jiraMutatedMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), path) {
		t.Fatalf("err = %v", msg.err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the file must survive a failed save")
	}
}

// TestEditCommentInPlace: your comment's edit replaces its body under its
// byline, the keys after it.
func TestEditCommentInPlace(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: &jira.Issue{
		Key: "ABC-1", Summary: "Fix the widget",
		Comments: []jira.Comment{{ID: "1", Author: "Ada", Body: "first"}, {ID: "2", Author: "Bob", Body: "second"}},
	}})
	m = out.(Model)
	out, _ = m.handleDescLoaded(descLoadedMsg{key: "ABC-1", comment: "1", md: "first draft"})
	m = out.(Model)
	v := m.View()
	c := ansi.Strip(v.Content)
	ada, ed, keys, bob := strings.Index(c, "Ada"), strings.Index(c, "┃ first draft"), strings.Index(c, "ctrl+s save"), strings.Index(c, "Bob")
	if strings.Contains(c, "Comment — ABC-1") || ada < 0 || ed < ada || keys < ed || bob < keys {
		t.Fatalf("the edit should replace Ada's comment in the thread:\n%s", c)
	}
	if v.Cursor == nil {
		t.Error("the terminal cursor should sit in the editor")
	}
}

// TestEditorHighlightsMarkdown: the editor styles bold text and keeps its
// markers.
func TestEditorHighlightsMarkdown(t *testing.T) {
	m := loadedJiraModel(t)
	out, _ := m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "a **bold** b"})
	m = out.(Model)
	v := m.descEdit.input.View()
	if !strings.Contains(ansi.Strip(v), "a **bold** b") || !strings.Contains(v, lipgloss.NewStyle().Bold(true).Inline(true).Render("bold")) {
		t.Fatalf("bold not highlighted: %q", v)
	}
	m.openJiraCommentInput()
	if !m.jiraCommentInput.MarkdownHighlight {
		t.Error("the comment composer should highlight too")
	}
}
