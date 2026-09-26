package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/jira"
)

// E in the panel edits the description as markdown in the in-app editor
// (ctrl+s saves, ctrl+e hands it to $VISUAL / $EDITOR). Only a description
// markdown can carry both ways is offered (see jira.EditableDescription);
// saving it unchanged writes nothing. Rich-text fields and your comments
// edit the same way.

// descEdit is the in-app editor open on a description, field or comment.
type descEdit struct {
	key, comment, field, before string
	kept                        []json.RawMessage
	input                       editor.Model
	discard                     bool // a first esc on changed text asked to confirm
}

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

// handleDescLoaded opens the in-app editor on the markdown.
func (m Model) handleDescLoaded(msg descLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.key + ": " + msg.err.Error() + " — edit it in Jira (o)")
		return m, nil
	}
	ed := newModalComposer("")
	ed.MaxHeight = max(m.bodyH()-12, 6)
	ed.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("enter", "alt+enter", "shift+enter"))
	ed.SetValue(msg.md)
	m.descEdit = &descEdit{key: msg.key, comment: msg.comment, field: msg.field, before: msg.md, kept: msg.kept, input: ed}
	m.status = ""
	if m.descEditInline() {
		m.descEdit.input.MaxHeight = max(m.refView.Height()-4, 6)
		m.renderRef()
		m.showInlineEditor()
	}
	return m, nil
}

// descEditMark stands in the panel's content for the inline editor, which
// renderRef puts in after the tables and images are laid out.
const descEditMark = "\x00descedit\x00"

// descEditHint is the editor's keys.
const descEditHint = "ctrl+s save · ctrl+e $EDITOR · esc cancel"

// descEditInline is whether the editor sits in the panel's body, in place of
// the description, field or comment it edits.
func (m *Model) descEditInline() bool {
	d := m.descEdit
	return d != nil && m.refOpen && m.jiraIssue != nil && m.jiraIssue.Key == d.key && m.refErr == nil && !m.refLoading
}

// descEditOn is whether the inline editor is on field ("" the description).
func (m *Model) descEditOn(field string) bool {
	return m.descEditInline() && m.descEdit.comment == "" && m.descEdit.field == field
}

// commentEditOn is whether the inline editor is on the comment id.
func (m *Model) commentEditOn(id string) bool {
	return m.descEditInline() && id != "" && m.descEdit.comment == id
}

// commentMark stands in the same way for the comment composer; its line
// may carry the reply's indent after it.
const commentMark = "\x00comment\x00"

// inlineEditor is the editor drawn in the panel's body, nil for none, and
// how many reply bars indent it.
func (m *Model) inlineEditor() (ed *editor.Model, indent int) {
	switch {
	case m.descEditInline():
		return &m.descEdit.input, m.commentIndent
	case m.commentInline():
		return &m.jiraCommentInput, m.commentIndent
	}
	return nil, 0
}

// placeInlineEditor swaps the mark for the inline editor, recording its
// line; a comment's composer brings its mention list and keys.
func (m *Model) placeInlineEditor(content string, width int) string {
	m.inlineLine = -1
	ed, indent := m.inlineEditor()
	if ed == nil {
		return content
	}
	mark := descEditMark
	if ed == &m.jiraCommentInput {
		mark = commentMark
	}
	lines := strings.Split(content, "\n")
	i := slices.Index(lines, mark)
	if i < 0 {
		return content
	}
	m.inlineLine = i
	ed.SetWidth(max(width-2*indent, 8))
	view := strings.Split(ed.View(), "\n")
	if mark == commentMark {
		if list := m.renderMentions(); list != "" {
			view = append(view, strings.Split(list, "\n")...)
		}
		view = append(view, refDimStyle.Render("↵ post · alt+↵ newline · @ mention · esc cancel"))
	} else if m.descEdit.comment != "" {
		view = append(view, refDimStyle.Render(descEditHint))
	}
	bars := refDimStyle.Render(strings.Repeat("│ ", indent))
	for j := range view {
		view[j] = bars + view[j]
	}
	return strings.Join(slices.Concat(lines[:i], view, lines[i+1:]), "\n")
}

// inlineEditorCursor is the inline editor's cursor on screen, when it is in
// view.
func (m *Model) inlineEditorCursor() (x, y int, ok bool) {
	ed, indent := m.inlineEditor()
	if ed == nil || m.inlineLine < 0 {
		return 0, 0, false
	}
	cx, cy, ok := ed.CursorViewPos()
	if !ok {
		return 0, 0, false
	}
	row := m.inlineEditorRow() + cy - m.refView.YOffset()
	if row < 0 || row >= m.refView.Height() {
		return 0, 0, false
	}
	listW, _ := m.jiraListWidth(m.width)
	return listW + 1 + 2*indent + cx, 1 + m.crumbRows() + row, true
}

// inlineEditorRow is the editor's first row in the panel's wrapped content.
func (m *Model) inlineEditorRow() int {
	return visualRowsBefore(strings.Split(m.refView.GetContent(), "\n"), m.inlineLine, m.refView.Width())
}

// showInlineEditor scrolls the panel to keep the inline editor's cursor in
// view, a line or two above it too when the cursor allows.
func (m *Model) showInlineEditor() {
	ed, _ := m.inlineEditor()
	if ed == nil || m.inlineLine < 0 {
		return
	}
	_, cy, _ := ed.CursorViewPos()
	row, top, h := m.inlineEditorRow()+cy, m.refView.YOffset(), m.refView.Height()
	switch {
	case row < top:
		m.refView.SetYOffset(max(row-2, 0))
	case row >= top+h:
		m.refView.SetYOffset(row - h + 3) // the keys line below it too
	}
}

// descEditTitle names what the editor is on.
func (d *descEdit) title() string {
	switch {
	case d.comment != "":
		return "Comment — " + d.key
	case d.field != "":
		return "Field — " + d.key
	}
	return "Description — " + d.key
}

func (m Model) handleDescEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.descEdit
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		if d.input.Value() != d.before && !d.discard {
			d.discard = true
			m.status = "esc again discards your changes · ctrl+s saves"
			return m, nil
		}
		m.descEdit, m.status = nil, ""
		return m, nil
	case "ctrl+s":
		m.descEdit = nil
		return m.saveDesc(descEditedMsg{key: d.key, comment: d.comment, field: d.field, before: d.before, kept: d.kept}, d.input.Value())
	case "ctrl+e":
		m.descEdit = nil
		return m.openExternalEditor(descLoadedMsg{key: d.key, comment: d.comment, field: d.field, md: d.input.Value(), kept: d.kept}, d.before)
	}
	d.discard = false
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return m, cmd
}

func (m *Model) renderDescEdit() string {
	d := m.descEdit
	return m.renderModalComposer(d.title(), nil, "ctrl+s save · ctrl+e $EDITOR · esc cancel", &d.input)
}

// openExternalEditor writes md to a file and opens $VISUAL / $EDITOR on it;
// before is the text as Jira has it, to tell a change.
func (m Model) openExternalEditor(msg descLoadedMsg, before string) (tea.Model, tea.Cmd) {
	f, err := os.CreateTemp("", "laneway-"+msg.key+"-*.md")
	if err == nil {
		_, err = f.WriteString(msg.md + "\n")
		err = firstErr(err, f.Close())
	}
	if err != nil {
		m.fail("description: " + err.Error())
		return m, nil
	}
	m.status = "editing " + msg.key + " description…"
	key, comment, field, path, kept := msg.key, msg.comment, msg.field, f.Name(), msg.kept
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
	if msg.err != nil {
		os.Remove(msg.path)
		m.fail("editor: " + msg.err.Error())
		return m, nil
	}
	b, err := os.ReadFile(msg.path)
	if err != nil {
		m.fail("description: " + err.Error())
		return m, nil
	}
	return m.saveDesc(msg, string(b))
}

// saveDesc writes text back when it changed. With msg.path the text came
// from that file, which goes only once Jira has it; from the in-app editor
// a failed save puts the text in a file to keep it.
func (m Model) saveDesc(msg descEditedMsg, text string) (tea.Model, tea.Cmd) {
	after := strings.TrimSpace(text)
	if after == strings.TrimSpace(msg.before) {
		if msg.path != "" {
			os.Remove(msg.path)
		}
		m.status = msg.key + " unchanged"
		return m, nil
	}
	c, ctx, key, kept, comment, path := m.jiraClient, m.ctx, msg.key, msg.kept, msg.comment, msg.path
	keep := func(err error) error {
		switch {
		case err != nil && path == "":
			f, ferr := os.CreateTemp("", "laneway-"+key+"-*.md")
			if ferr != nil {
				return err
			}
			_, _ = f.WriteString(after + "\n")
			f.Close()
			return fmt.Errorf("%w — your text is kept in %s", err, f.Name())
		case err != nil:
			return fmt.Errorf("%w — your text is kept in %s", err, path)
		case path != "":
			os.Remove(path)
		}
		return nil
	}
	if field := msg.field; field != "" {
		var doc any // blank clears
		if after != "" {
			doc = jira.MarkdownToADFKept(after, kept)
		}
		m.status = "saving " + key + " " + field + "…"
		return m, jiraMutateCmd(key, field, func() error { return keep(c.SetField(ctx, key, field, doc)) })
	}
	if comment != "" {
		m.status = "saving the comment on " + key + "…"
		return m, jiraMutateCmd(key, "comment", func() error { return keep(c.SetComment(ctx, key, comment, after, kept)) })
	}
	m.status = "saving " + key + " description…"
	return m, jiraMutateCmd(key, "description", func() error { return keep(c.SetDescription(ctx, key, after, kept)) })
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
