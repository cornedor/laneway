package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// E in the panel edits the description in $VISUAL / $EDITOR as markdown.
// Only a description markdown can carry both ways is offered (see
// jira.EditableDescription); saving an unchanged file writes nothing.

// descLoadedMsg is the description fetched for editing.
type descLoadedMsg struct {
	key     string
	comment string // the comment being edited, "" for the description
	field   string // or the multi-line field being edited
	md      string
	// kept are the blocks the markdown holds as placeholder lines.
	kept []json.RawMessage
	err  error
}

// descEditedMsg is the editor closed on path.
type descEditedMsg struct {
	key, comment, field, path, before string
	kept                              []json.RawMessage
	err                               error
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
		ed, err := jira.EditableDescription(raw)
		return descLoadedMsg{key: key, md: ed.Markdown, kept: ed.Kept, err: err}
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
	key, comment, field, path, before, kept := msg.key, msg.comment, msg.field, f.Name(), msg.md, msg.kept
	return m, tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
		return descEditedMsg{key: key, comment: comment, field: field, path: path, before: before, kept: kept, err: err}
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
	c, ctx, key, kept, comment := m.jiraClient, m.ctx, msg.key, msg.kept, msg.comment
	if field := msg.field; field != "" {
		var doc any // blank clears
		if after != "" {
			doc = jira.MarkdownToADFKept(after, kept)
		}
		m.status = "saving " + key + " " + field + "…"
		return m, jiraMutateCmd(key, field, func() error { return c.SetField(ctx, key, field, doc) })
	}
	if comment != "" {
		m.status = "saving the comment on " + key + "…"
		return m, jiraMutateCmd(key, "comment", func() error { return c.SetComment(ctx, key, comment, after, kept) })
	}
	m.status = "saving " + key + " description…"
	return m, jiraMutateCmd(key, "description", func() error { return c.SetDescription(ctx, key, after, kept) })
}

// openCommentPicker lists your own comments on the panel issue to edit.
func (m *Model) openCommentPicker() tea.Cmd {
	if m.jiraIssue == nil {
		return nil
	}
	iss := m.jiraIssue
	gen := m.startJiraPicker(jiraPickEditComment, "Edit a comment on "+iss.Key, false)
	seq, c, ctx := m.jiraPicker.fetchSeq, m.jiraClient, m.ctx
	comments := iss.Comments
	return func() tea.Msg {
		me, err := c.Myself(ctx)
		var items []jiraPickerItem
		for i := len(comments) - 1; i >= 0; i-- { // newest first
			cm := comments[i]
			if err == nil && cm.AuthorID == me.AccountID && cm.ID != "" {
				items = append(items, jiraPickerItem{id: strconv.Itoa(i), label: commentPickerLabel(cm)})
			}
		}
		if err == nil && len(items) == 0 {
			items = []jiraPickerItem{{id: "", label: "no comments of yours here"}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickEditComment, items: items, err: err}
	}
}

// editComment opens comment i of the panel issue in the editor.
func (m *Model) editComment(i int) tea.Cmd {
	if m.jiraIssue == nil || i < 0 || i >= len(m.jiraIssue.Comments) {
		return nil
	}
	key, cm := m.jiraIssue.Key, m.jiraIssue.Comments[i]
	return func() tea.Msg {
		ed, err := jira.EditableDescription(cm.Raw)
		return descLoadedMsg{key: key, comment: cm.ID, md: ed.Markdown, kept: ed.Kept, err: err}
	}
}
