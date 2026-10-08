package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// Private notes: N in the panel opens notes/ABC-12.md in $EDITOR. They stay
// on this machine, plain files beside the state file; the panel shows them
// folded, is:notes finds them, and A can post them as a comment.

type notesEditedMsg struct {
	key string
	err error
}

// notesDir holds the site's notes: notes beside state.json, notes-work
// beside state-work.json. "" without a store.
func (m *Model) notesDir() string {
	if m.store == nil {
		return ""
	}
	p := m.store.Path()
	base := strings.TrimSuffix(filepath.Base(p), ".json")
	return filepath.Join(filepath.Dir(p), "notes"+strings.TrimPrefix(base, "state"))
}

// notesPath is key's notes file, "" without a store.
func (m *Model) notesPath(key string) string {
	d := m.notesDir()
	if d == "" || !jira.ValidKey(key) {
		return ""
	}
	return filepath.Join(d, key+".md")
}

// notes is key's notes, trimmed; "" when there are none.
func (m *Model) notes(key string) string {
	p := m.notesPath(key)
	if p == "" {
		return ""
	}
	raw, _ := os.ReadFile(p)
	return strings.TrimSpace(string(raw))
}

// notedKeys is every key with notes, for is:notes.
func (m *Model) notedKeys() map[string]bool {
	d := m.notesDir()
	if d == "" {
		return nil
	}
	entries, _ := os.ReadDir(d)
	out := map[string]bool{}
	for _, e := range entries {
		if k, ok := strings.CutSuffix(e.Name(), ".md"); ok && !e.IsDir() {
			out[strings.ToUpper(k)] = true
		}
	}
	return out
}

// editNotes opens key's notes in the editor, making the folder first.
func (m *Model) editNotes(key string) tea.Cmd {
	p := m.notesPath(key)
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		m.fail(i18n.Tf("notes: %s", err.Error()))
		return nil
	}
	m.status = i18n.Tf("editing your notes on %s…", key)
	return tea.ExecProcess(editorCommand(p), func(err error) tea.Msg {
		return notesEditedMsg{key: key, err: err}
	})
}

// handleNotesEdited drops an emptied file and redraws the panel.
func (m Model) handleNotesEdited(msg notesEditedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(i18n.Tf("notes: %s", msg.err.Error()))
		return m, nil
	}
	if p := m.notesPath(msg.key); m.notes(msg.key) == "" {
		_ = os.Remove(p)
		m.status = i18n.Tf("no notes on %s", msg.key)
	} else {
		m.status = i18n.Tf("notes on %s saved, on this machine only", msg.key)
	}
	m.renderRef()
	m.applyJiraSearch()
	return m, nil
}

// renderNotes writes the folded "Notes (local)" section: the first line and
// how many more.
func (m *Model) renderNotes(b *strings.Builder, key string, width int) {
	text := m.notes(key)
	if text == "" {
		return
	}
	lines := strings.Split(text, "\n")
	b.WriteString(sectionHead(i18n.T("Notes (local)"), i18n.Tf("  %s edit", helpKey(m.keys.Notes)), width))
	b.WriteString(truncate(lines[0], max(width, 1)) + "\n")
	if n := len(lines) - 1; n > 0 {
		b.WriteString(refDimStyle.Render(i18n.Tn(n, "+%d more line", "+%d more lines", n)) + "\n")
	}
}

// postNotes opens the comment composer holding the notes, to post as they
// are or edited.
func (m *Model) postNotes() {
	text := m.notes(m.jiraIssue.Key)
	m.openJiraCommentInput()
	m.jiraCommentInput.SetValue(text)
	m.jiraCommentInput.CursorEnd()
	m.status = i18n.T("your notes, as a comment · ctrl+s posts")
}
