package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// TestNotes: notes live beside the state file, show folded in the panel,
// match is:notes and post through the comment composer.
func TestNotes(t *testing.T) {
	m := jiraTabModel(t)
	dir := filepath.Join(filepath.Dir(m.store.Path()), "notes")
	if m.notesDir() != dir {
		t.Fatalf("dir %q, want %q", m.notesDir(), dir)
	}
	if cmd := m.editNotes("ABC-3"); cmd == nil {
		t.Fatal("no editor")
	}
	if err := os.WriteFile(m.notesPath("ABC-3"), []byte("ask Ada about the API\nsecond\nthird\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"/", "h", "a", "s", ":", "n", "o", "t", "e", "s", "enter"} {
		out, _ := m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	if len(m.jiraTab.order) != 1 || m.jiraTab.cards[m.jiraTab.order[0]].Key != "ABC-3" {
		t.Fatalf("has:notes order %v, want only ABC-3", m.jiraTab.order)
	}

	m.jiraIssue = &jira.Issue{Key: "ABC-3", Summary: "Third"}
	view := ansi.Strip(m.renderJiraIssue(m.jiraIssue, 80))
	if !strings.Contains(view, "Notes (local)") || !strings.Contains(view, "ask Ada about the API") || !strings.Contains(view, "+2 more lines") || strings.Contains(view, "second") {
		t.Errorf("panel lacks the folded notes:\n%s", view)
	}

	m.openIssueActions()
	if !slices.ContainsFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == "post-notes" }) {
		t.Fatal("no post-notes action")
	}
	m.closeJiraPicker()
	m.applyIssueAction("ABC-3", "post-notes")
	if !m.jiraCommentActive || !strings.HasPrefix(m.jiraCommentInput.Value(), "ask Ada") {
		t.Errorf("composer %v %q", m.jiraCommentActive, m.jiraCommentInput.Value())
	}

	if err := os.WriteFile(m.notesPath("ABC-3"), []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := m.handleNotesEdited(notesEditedMsg{key: "ABC-3"})
	m = out.(Model)
	if _, err := os.Stat(m.notesPath("ABC-3")); !os.IsNotExist(err) {
		t.Errorf("emptied notes kept: %v", err)
	}
}

// TestNotesPathBadKey: a key that is not one gets no notes file.
func TestNotesPathBadKey(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{store: st}
	for _, key := range []string{"../../.bashrc-1", "A/B-1", ""} {
		if p := m.notesPath(key); p != "" {
			t.Errorf("notesPath(%q) = %q", key, p)
		}
	}
}

// TestNotesDirSite: another site's notes sit in their own folder.
func TestNotesDirSite(t *testing.T) {
	d := t.TempDir()
	st, err := store.Open(filepath.Join(d, "state-work.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{store: st}
	if got := m.notesDir(); got != filepath.Join(d, "notes-work") {
		t.Errorf("dir %q", got)
	}
}
