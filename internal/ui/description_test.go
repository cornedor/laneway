package ui

import (
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
