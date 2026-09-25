package ui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// E in the panel edits the description in $VISUAL / $EDITOR as markdown.
// Only a description markdown can carry both ways is offered (see
// jira.EditableDescription); saving an unchanged file writes nothing.

// descLoadedMsg is the description fetched for editing.
type descLoadedMsg struct {
	key string
	md  string
	err error
}

// descEditedMsg is the editor closed on path.
type descEditedMsg struct {
	key, path, before string
	err               error
}

// editDescription fetches the panel issue's description for the editor.
func (m *Model) editDescription() tea.Cmd {
	if m.jiraIssue == nil {
		return nil
	}
	key, c, ctx := m.jiraIssue.Key, m.jiraClient, m.ctx
	m.status = "loading " + key + " description…"
	return func() tea.Msg {
		raw, err := c.Description(ctx, key)
		if err != nil {
			return descLoadedMsg{key: key, err: err}
		}
		md, err := jira.EditableDescription(raw)
		return descLoadedMsg{key: key, md: md, err: err}
	}
}

// handleDescLoaded writes the markdown to a file and opens the editor on it.
func (m Model) handleDescLoaded(msg descLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.key + ": " + msg.err.Error() + " — edit it in Jira (o)"
		return m, nil
	}
	f, err := os.CreateTemp("", "laneway-"+msg.key+"-*.md")
	if err == nil {
		_, err = f.WriteString(msg.md + "\n")
		err = firstErr(err, f.Close())
	}
	if err != nil {
		m.status = "description: " + err.Error()
		return m, nil
	}
	m.status = "editing " + msg.key + " description…"
	key, path, before := msg.key, f.Name(), msg.md
	return m, tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
		return descEditedMsg{key: key, path: path, before: before, err: err}
	})
}

// editorCommand opens path in $VISUAL, else $EDITOR, else vi. The variable
// may carry arguments ("code -w").
func editorCommand(path string) *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	args := strings.Fields(ed)
	if len(args) == 0 {
		args = []string{"vi"}
	}
	return exec.Command(args[0], append(args[1:], path)...)
}

// handleDescEdited saves the file back when it changed.
func (m Model) handleDescEdited(msg descEditedMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.status = "editor: " + msg.err.Error()
		return m, nil
	}
	b, err := os.ReadFile(msg.path)
	if err != nil {
		m.status = "description: " + err.Error()
		return m, nil
	}
	after := strings.TrimSpace(string(b))
	if after == strings.TrimSpace(msg.before) {
		m.status = msg.key + " description unchanged"
		return m, nil
	}
	c, ctx, key := m.jiraClient, m.ctx, msg.key
	m.status = "saving " + key + " description…"
	return m, jiraMutateCmd(key, "description", func() error { return c.SetDescription(ctx, key, after) })
}
