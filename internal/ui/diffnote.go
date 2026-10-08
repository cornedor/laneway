package ui

import (
	"fmt"
	"github.com/cornedor/laneway/internal/i18n"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// The inline note: the write half of the diff view. c on a line takes a
// paragraph of markdown into your pending review (GitLab's draft notes) as a
// conversation anchored to that line; on a conversation it replies to it. x
// drops a pending note, S submits the review with a verdict and a summary, A
// approves on its own. R resolves the conversation at the cursor, or reopens
// it.
//
// Ported from matterbox's internal/ui/diffnote.go.

// diffNoteState is the composer. It lives inside diffState (which is already
// behind a pointer), so the editor's buffer costs Model nothing.
type diffNoteState struct {
	active bool
	input  editor.Model

	// Where the note goes. replyTo names an existing conversation; when it is
	// empty the position fields anchor a new one.
	replyTo string
	oldPath string
	newPath string
	oldLine int
	newLine int

	// context is the line the note is about, shown above the editor so you can
	// see what you are commenting on while you type it.
	context string
	// submit is a review's verdict (forge.Verdict*): the text is its summary.
	submit string
	// lines is the range a multi-line note covers (V), ending on its line.
	lines *forge.LineRange
	// edit is the pending note being changed (E), 0 for a new one.
	edit int
}

// diffNotePostedMsg carries the result of posting an inline note.
type diffNotePostedMsg struct {
	gen   int
	reply bool
	err   error
}

// diffNoteActive reports whether the note composer is up.
func (m *Model) diffNoteActive() bool {
	return m.diff != nil && m.diff.note.active
}

// openDiffNote raises the composer for the row under the cursor: a reply on a
// note row, a new conversation on a code row, and nothing at all on a file or
// hunk header — there is no line there to anchor to.
func (m Model) openDiffNote() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil || d.cursor < 0 || d.cursor >= len(d.rows) {
		return m, nil
	}
	if d.diff == nil {
		return m, nil
	}
	r := d.rows[d.cursor]
	note := diffNoteState{active: true, input: newModalComposer(i18n.T("note…"))}
	switch {
	case r.kind == diffRowNote:
		if r.thread < 0 || r.thread >= len(d.threads) {
			return m, nil
		}
		t := d.threads[r.thread]
		note.replyTo = t.ID
		where := t.Path
		if where == "" {
			where = i18n.T("the merge request")
		}
		note.context = i18n.Tf("↩ replying to %s on %s", threadAuthor(t), where)
	case r.kind.commentable():
		f := d.diff.Files[r.file]
		note.newPath, note.oldPath = f.NewPath, f.OldPath
		if note.newPath == "" {
			note.newPath = f.Path()
		}
		if note.oldPath == "" {
			note.oldPath = f.Path()
		}
		note.oldLine, note.newLine = r.old, r.new
		note.context = f.Path() + ":" + lineLabel(r) + "  " + strings.TrimSpace(ansi.Strip(r.text))
	default:
		m.status = i18n.T("no line here to comment on — move to a line of the diff")
		return m, nil
	}
	d.note = note
	if r.kind.commentable() {
		d.withMark()
	}
	return m, nil
}

// threadAuthor is who started a conversation, for the "replying to" line.
func threadAuthor(t forge.Thread) string {
	if len(t.Notes) == 0 || t.Notes[0].Author == "" {
		return i18n.T("the thread")
	}
	return t.Notes[0].Author
}

// lineLabel names the line a note will hang off the way the forge thinks of it:
// the new-side number when the line exists there, otherwise the old one.
func lineLabel(r diffRow) string {
	if r.new > 0 {
		return strconv.Itoa(r.new)
	}
	return strconv.Itoa(r.old)
}

// closeDiffNote tears the composer down without posting.
func (m *Model) closeDiffNote() {
	if m.diff == nil {
		return
	}
	m.diff.note = diffNoteState{}
}

// handleDiffNoteKey owns every keystroke while the composer is open: esc
// cancels, Enter posts, alt/shift+enter insert a newline, everything else edits
// the text. Same contract as the Jira comment composer next door.
func (m Model) handleDiffNoteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.diff == nil {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.closeDiffNote()
		return m, nil
	case "enter":
		return m.applyDiffNote()
	}
	var cmd tea.Cmd
	m.diff.note.input, cmd = m.diff.note.input.Update(msg)
	return m, cmd
}

// applyDiffNote closes the composer and posts the note. An empty body is a
// cancel — a blank comment is never what was meant.
func (m Model) applyDiffNote() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil {
		return m, nil
	}
	n := d.note
	text := strings.TrimSpace(strings.ReplaceAll(n.input.Value(), suggestTab, "\t"))
	m.closeDiffNote()
	if n.submit != "" {
		return m, m.submitReview(n.submit, text)
	}
	if n.edit != 0 {
		if text == "" {
			return m, nil
		}
		c, ctx, repo, number, gen := d.c, m.ctx, d.repo, d.number, d.gen
		m.status = i18n.T("changing the pending note…")
		return m, func() tea.Msg {
			return diffReviewedMsg{gen: gen, what: i18n.T("pending note changed"), err: c.EditDraft(ctx, repo, number, n.edit, text)}
		}
	}
	if text == "" {
		return m, nil
	}
	rv := d.c
	note := forge.NewNote{
		Body:    text,
		ReplyTo: n.replyTo,
		Refs:    d.diff.Refs,
		OldPath: n.oldPath,
		NewPath: n.newPath,
		OldLine: n.oldLine,
		NewLine: n.newLine,
		Range:   n.lines,
	}
	ctx, repo, number, gen := m.ctx, d.repo, d.number, d.gen
	reply := n.replyTo != ""
	m.status = i18n.T("adding to your review…")
	return m, func() tea.Msg {
		return diffNotePostedMsg{gen: gen, reply: reply, err: rv.AddDraft(ctx, repo, number, note)}
	}
}

// handleDiffNotePosted reports the outcome and, on success, refetches the
// conversations so the new note shows where it landed. The diff itself is a
// cache hit, so that reload is one request.
func (m Model) handleDiffNotePosted(msg diffNotePostedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = i18n.Tf("note failed: %s", msg.err.Error())
		return m, nil
	}
	if m.diff == nil || msg.gen != m.diff.gen {
		m.status = i18n.T("in your review")
		return m, nil
	}
	m.status = i18n.T("in your review: S submits it")
	return m, m.reloadDiffThreads()
}

// renderDiffNote draws the composer, in the same box the Jira comment
// composer uses. It sits over the foot of the diff (diffNoteTop), the line
// it is about kept in sight above it.
func (m *Model) renderDiffNote() string {
	if !m.diffNoteActive() {
		return ""
	}
	n := &m.diff.note
	title, hint := i18n.Tf("Note — %s", m.diff.label), i18n.T("↵ add to your review · alt+↵ newline · esc cancel")
	switch {
	case n.edit != 0:
		title, hint = i18n.Tf("Pending note — %s", m.diff.label), i18n.T("↵ save · alt+↵ newline · esc cancel")
	case n.submit != "":
		title, hint = i18n.Tf("Submit review — %s", m.diff.label), i18n.T("↵ submit (empty: no summary) · alt+↵ newline · esc cancel")
	case n.replyTo != "":
		title = i18n.Tf("Reply — %s", m.diff.label)
	}
	var above []string
	if n.context != "" {
		above = append(above, lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render(n.context))
	}
	return m.renderModalComposer(title, above, hint, &n.input)
}

// diffNoteCursor places the terminal cursor in the composer, drawn as box.
func (m *Model) diffNoteCursor(box string) (col, row int, ok bool) {
	if !m.diffNoteActive() {
		return 0, 0, false
	}
	above := 0
	if m.diff.note.context != "" {
		above = 1
	}
	return m.modalComposerCursorAt(above, &m.diff.note.input, box, m.diffNoteTop(box))
}

// diffNoteTop is the body row the composer box starts on: low in the diff
// view, its last row on the diff's last, above the view's hint and border.
func (m *Model) diffNoteTop(box string) int {
	return max(m.bodyH()-2-lipgloss.Height(box), 0)
}

// overDiff draws the composer box over the diff view body.
func (m *Model) overDiff(body, box string) string {
	x := placeOffset(m.width, lipgloss.Width(box))
	return lipgloss.NewCompositor(lipgloss.NewLayer(body), lipgloss.NewLayer(box).X(x).Y(m.diffNoteTop(box)).Z(1)).Render()
}

// --- resolving -------------------------------------------------------------

// diffResolvedMsg carries the result of resolving or reopening a conversation.
type diffResolvedMsg struct {
	gen      int
	resolved bool
	err      error
}

// threadAtCursor is the conversation the cursor is pointing at: the one under
// it on a note row, or the first one hanging off the code line it is on. The
// second case is the one that matters in practice — you read a line, you resolve
// what was said about it, without stepping into the note first.
func (d *diffState) threadAtCursor() int {
	if d.cursor < 0 || d.cursor >= len(d.rows) {
		return -1
	}
	if r := d.rows[d.cursor]; r.kind == diffRowNote {
		return r.thread
	}
	if !d.rows[d.cursor].kind.commentable() {
		return -1
	}
	for i := d.cursor + 1; i < len(d.rows) && d.rows[i].kind == diffRowNote; i++ {
		if d.rows[i].noteHead {
			return d.rows[i].thread
		}
	}
	return -1
}

// toggleDiffResolve resolves the conversation at the cursor, or reopens it when
// it is already resolved.
func (m Model) toggleDiffResolve() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil {
		return m, nil
	}
	ti := d.threadAtCursor()
	if ti < 0 || ti >= len(d.threads) {
		m.status = i18n.T("no inline thread here to resolve")
		return m, nil
	}
	rv := d.c
	t := d.threads[ti]
	if !t.Resolvable && !t.Resolved {
		m.status = i18n.T("a plain comment: nothing to resolve")
		return m, nil
	}
	want := !t.Resolved
	ctx, repo, number, gen := m.ctx, d.repo, d.number, d.gen
	if want {
		m.status = i18n.T("resolving thread…")
	} else {
		m.status = i18n.T("reopening thread…")
	}
	return m, func() tea.Msg {
		return diffResolvedMsg{gen: gen, resolved: want, err: rv.ResolveThread(ctx, repo, number, t.ID, want)}
	}
}

// handleDiffResolved reports the outcome and refetches the conversations, so
// the thread's new state is the forge's answer rather than our guess.
func (m Model) handleDiffResolved(msg diffResolvedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = i18n.Tf("resolve failed: %s", msg.err.Error())
		return m, nil
	}
	m.status = i18n.T("thread resolved")
	if !msg.resolved {
		m.status = i18n.T("thread reopened")
	}
	if m.diff == nil || msg.gen != m.diff.gen {
		return m, nil
	}
	return m, m.reloadDiffThreads()
}

// --- the review ------------------------------------------------------------

// reviewVerdicts are S's choices, in order.
var reviewVerdicts = []struct{ id, label string }{
	{forge.VerdictComment, i18n.N("Comment")},
	{forge.VerdictApprove, i18n.N("Approve")},
	{forge.VerdictChanges, i18n.N("Request changes")},
}

// verdictLines are S's list, the cursor's row marked.
func (d *diffState) verdictLines(width int) []string {
	head := i18n.T("Submit your review")
	if n := len(d.drafts); n > 0 {
		head += ": " + i18n.Tn(n, "%d pending note", "%d pending notes", n)
	}
	lines := []string{refKeyStyle.Render(head) + refDimStyle.Render(i18n.T("  ↵ pick, then a summary · esc close"))}
	for i, v := range reviewVerdicts {
		line := truncate("  "+i18n.T(v.label), width)
		if i == d.verdict {
			line = selectedRow.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

// handleDiffVerdictKey is S's list: enter asks for the summary.
func (m Model) handleDiffVerdictKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "q", "S":
		d.verdicts = false
	case "up", "k":
		d.verdict = max(d.verdict-1, 0)
	case "down", "j":
		d.verdict = min(d.verdict+1, len(reviewVerdicts)-1)
	case "enter":
		d.verdicts = false
		v := reviewVerdicts[d.verdict]
		d.note = diffNoteState{active: true, input: newModalComposer(i18n.T("a summary, or nothing")), submit: v.id,
			context: i18n.T(v.label) + " · " + i18n.Tn(len(d.drafts), "%d pending note", "%d pending notes", len(d.drafts))}
	}
	return m, nil
}

// diffReviewedMsg is a review submitted, or an approval given.
type diffReviewedMsg struct {
	gen  int
	what string
	err  error
}

// submitReview publishes the pending notes with summary and verdict.
func (m *Model) submitReview(verdict, summary string) tea.Cmd {
	d := m.diff
	c, ctx, repo, number, gen := d.c, m.ctx, d.repo, d.number, d.gen
	m.status = i18n.T("submitting your review…")
	what := map[string]string{forge.VerdictComment: i18n.T("review submitted"), forge.VerdictApprove: i18n.T("review submitted, approved"),
		forge.VerdictChanges: i18n.T("review submitted, changes requested")}[verdict]
	return func() tea.Msg {
		return diffReviewedMsg{gen: gen, what: what, err: c.SubmitReview(ctx, repo, number, summary, verdict)}
	}
}

// approveMR approves the merge request on its own, notes pending or not.
func (m *Model) approveMR(c *gitlab.Client, repo string, number int) tea.Cmd {
	gen := 0
	if m.diff != nil {
		gen = m.diff.gen
	}
	ctx := m.ctx
	m.status = i18n.Tf("approving %s!%d…", repo, number)
	return func() tea.Msg {
		return diffReviewedMsg{gen: gen, what: i18n.T("approved"), err: c.Approve(ctx, repo, number)}
	}
}

func (m Model) handleDiffReviewed(msg diffReviewedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = i18n.Tf("review failed: %s", msg.err.Error())
		return m, nil
	}
	m.status = msg.what
	var cmds []tea.Cmd
	if p := m.mr; p != nil && p.mr != nil { // the panel's merge request: its approvals moved
		cmds = append(cmds, m.openMR(p.c, p.ref, p.link, p.title, true))
	}
	if m.diff != nil && msg.gen == m.diff.gen {
		cmds = append(cmds, m.reloadDiffThreads())
	}
	return m, tea.Batch(cmds...)
}

// editDiffDraft opens the composer on the pending note under the cursor (E),
// an agent's finding to reword before the review goes out.
func (m Model) editDiffDraft() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil || d.cursor >= len(d.rows) || d.rows[d.cursor].draft == 0 {
		m.status = i18n.T("no pending note here")
		return m, nil
	}
	dr := d.drafts[d.rows[d.cursor].draft-1]
	d.note = diffNoteState{active: true, input: newModalComposer(i18n.T("note…")), edit: dr.ID, context: i18n.T("✎ your pending note")}
	d.note.input.SetValue(strings.ReplaceAll(dr.Body, "\t", suggestTab))
	return m, nil
}

// deleteDiffDraft drops the pending note under the cursor.
func (m Model) deleteDiffDraft() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil || d.cursor >= len(d.rows) || d.rows[d.cursor].draft == 0 {
		m.status = i18n.T("no pending note here")
		return m, nil
	}
	dr := d.drafts[d.rows[d.cursor].draft-1]
	c, ctx, repo, number, gen := d.c, m.ctx, d.repo, d.number, d.gen
	m.status = i18n.T("dropping the pending note…")
	return m, func() tea.Msg {
		return diffReviewedMsg{gen: gen, what: i18n.T("pending note dropped"), err: c.DeleteDraft(ctx, repo, number, dr.ID)}
	}
}

// --- ranges and suggestions ------------------------------------------------

// toggleDiffMark starts a range of lines at the cursor (V), or drops it: c
// then notes the lines from there to the cursor, s suggests a change to them.
func (m Model) toggleDiffMark() (tea.Model, tea.Cmd) {
	d := m.diff
	switch {
	case d.mark > 0:
		d.mark = 0
		m.status = ""
	case d.cursor < len(d.rows) && d.rows[d.cursor].kind.commentable():
		d.mark = d.cursor + 1
		m.status = i18n.T("a range from here: move to its end, c notes it, s suggests, esc drops it")
	default:
		m.status = i18n.T("V on a line of the diff starts a range")
	}
	return m, nil
}

// markRange is the marked range's first and last row, ok false without one
// or across files.
func (d *diffState) markRange() (from, to int, ok bool) {
	if d.mark == 0 || d.cursor >= len(d.rows) {
		return 0, 0, false
	}
	from, to = min(d.mark-1, d.cursor), max(d.mark-1, d.cursor)
	if d.rows[from].file != d.rows[to].file || !d.rows[from].kind.commentable() || !d.rows[to].kind.commentable() {
		return 0, 0, false
	}
	return from, to, true
}

// inMark is whether row i is in the marked range.
func (d *diffState) inMark(i int) bool {
	from, to, ok := d.markRange()
	return ok && i >= from && i <= to
}

// linePos is row r as a range end.
func linePos(r diffRow) forge.LinePos {
	return forge.LinePos{OldLine: r.old, NewLine: r.new, OldPos: r.oldPos, NewPos: r.newPos}
}

// withMark anchors the note being opened to the marked range, ending on its
// last line, and says so in its context line.
func (d *diffState) withMark() {
	from, to, ok := d.markRange()
	if !ok || from == to {
		return
	}
	end := d.rows[to]
	d.note.oldLine, d.note.newLine = end.old, end.new
	d.note.lines = &forge.LineRange{Start: linePos(d.rows[from]), End: linePos(end)}
	f := d.diff.Files[end.file]
	d.note.context = fmt.Sprintf("%s:%s–%s", f.Path(), lineLabel(d.rows[from]), lineLabel(end))
	d.mark = 0
}

// suggestTab stands for a tab in a suggestion while it is edited: the
// composer turns tabs into spaces, and a suggestion has to keep them.
const suggestTab = "⇥"

// openDiffSuggestion opens the composer on a suggested change to the line,
// or the marked range: its new side's text in a suggestion block to edit.
func (m Model) openDiffSuggestion() (tea.Model, tea.Cmd) {
	d := m.diff
	from, to, ok := d.markRange()
	if !ok {
		from, to = d.cursor, d.cursor
	}
	if from >= len(d.rows) || !d.rows[from].kind.commentable() {
		m.status = i18n.T("s on a line of the diff suggests a change to it")
		return m, nil
	}
	var lines []string
	for i := from; i <= to; i++ {
		switch r := d.rows[i]; {
		case r.kind == diffRowDel:
			m.status = i18n.T("a suggestion replaces new lines: leave the removed ones out of the range")
			return m, nil
		case r.kind.commentable():
			lines = append(lines, strings.ReplaceAll(r.raw, "\t", suggestTab))
		}
	}
	out, cmd := m.openDiffNote()
	m = out.(Model)
	if !m.diffNoteActive() {
		return m, cmd
	}
	m.diff.note.input.SetValue(fmt.Sprintf("```suggestion:-%d+0\n%s\n```", len(lines)-1, strings.Join(lines, "\n")))
	return m, cmd
}
