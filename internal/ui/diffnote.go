package ui

import (
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
	note := diffNoteState{active: true, input: newModalComposer("note…")}
	switch {
	case r.kind == diffRowNote:
		if r.thread < 0 || r.thread >= len(d.threads) {
			return m, nil
		}
		t := d.threads[r.thread]
		note.replyTo = t.ID
		where := t.Path
		if where == "" {
			where = "the merge request"
		}
		note.context = "↩ replying to " + threadAuthor(t) + " on " + where
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
		m.status = "no line here to comment on — move to a line of the diff"
		return m, nil
	}
	d.note = note
	return m, nil
}

// threadAuthor is who started a conversation, for the "replying to" line.
func threadAuthor(t forge.Thread) string {
	if len(t.Notes) == 0 || t.Notes[0].Author == "" {
		return "the thread"
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
	text := strings.TrimSpace(n.input.Value())
	m.closeDiffNote()
	if n.submit != "" {
		return m, m.submitReview(n.submit, text)
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
	}
	ctx, repo, number, gen := m.ctx, d.repo, d.number, d.gen
	reply := n.replyTo != ""
	m.status = "adding to your review…"
	return m, func() tea.Msg {
		return diffNotePostedMsg{gen: gen, reply: reply, err: rv.AddDraft(ctx, repo, number, note)}
	}
}

// handleDiffNotePosted reports the outcome and, on success, refetches the
// conversations so the new note shows where it landed. The diff itself is a
// cache hit, so that reload is one request.
func (m Model) handleDiffNotePosted(msg diffNotePostedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "note failed: " + msg.err.Error()
		return m, nil
	}
	if m.diff == nil || msg.gen != m.diff.gen {
		m.status = "in your review"
		return m, nil
	}
	m.status = "in your review: S submits it"
	return m, m.reloadDiffThreads()
}

// renderDiffNote draws the composer over the diff view, in the same box the
// Jira comment composer uses.
func (m *Model) renderDiffNote() string {
	if !m.diffNoteActive() {
		return ""
	}
	n := &m.diff.note
	title, hint := "Note — "+m.diff.label, "↵ add to your review · alt+↵ newline · esc cancel"
	switch {
	case n.submit != "":
		title, hint = "Submit review — "+m.diff.label, "↵ submit (empty: no summary) · alt+↵ newline · esc cancel"
	case n.replyTo != "":
		title = "Reply — " + m.diff.label
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
	return m.modalComposerCursor(above, &m.diff.note.input, box)
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
		m.status = "no inline thread here to resolve"
		return m, nil
	}
	rv := d.c
	t := d.threads[ti]
	if !t.Resolvable && !t.Resolved {
		m.status = "a plain comment: nothing to resolve"
		return m, nil
	}
	want := !t.Resolved
	ctx, repo, number, gen := m.ctx, d.repo, d.number, d.gen
	if want {
		m.status = "resolving thread…"
	} else {
		m.status = "reopening thread…"
	}
	return m, func() tea.Msg {
		return diffResolvedMsg{gen: gen, resolved: want, err: rv.ResolveThread(ctx, repo, number, t.ID, want)}
	}
}

// handleDiffResolved reports the outcome and refetches the conversations, so
// the thread's new state is the forge's answer rather than our guess.
func (m Model) handleDiffResolved(msg diffResolvedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "resolve failed: " + msg.err.Error()
		return m, nil
	}
	m.status = "thread resolved"
	if !msg.resolved {
		m.status = "thread reopened"
	}
	if m.diff == nil || msg.gen != m.diff.gen {
		return m, nil
	}
	return m, m.reloadDiffThreads()
}

// --- the review ------------------------------------------------------------

// reviewVerdicts are S's choices, in order.
var reviewVerdicts = []struct{ id, label string }{
	{forge.VerdictComment, "Comment"},
	{forge.VerdictApprove, "Approve"},
	{forge.VerdictChanges, "Request changes"},
}

// verdictLines are S's list, the cursor's row marked.
func (d *diffState) verdictLines(width int) []string {
	head := "Submit your review"
	if n := len(d.drafts); n > 0 {
		head += ": " + plural(n, "pending note")
	}
	lines := []string{refKeyStyle.Render(head) + refDimStyle.Render("  ↵ pick, then a summary · esc close")}
	for i, v := range reviewVerdicts {
		line := truncate("  "+v.label, width)
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
		d.note = diffNoteState{active: true, input: newModalComposer("a summary, or nothing"), submit: v.id,
			context: v.label + " · " + plural(len(d.drafts), "pending note")}
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
	m.status = "submitting your review…"
	what := map[string]string{forge.VerdictComment: "review submitted", forge.VerdictApprove: "review submitted, approved",
		forge.VerdictChanges: "review submitted, changes requested"}[verdict]
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
	m.status = "approving " + repo + "!" + strconv.Itoa(number) + "…"
	return func() tea.Msg {
		return diffReviewedMsg{gen: gen, what: "approved", err: c.Approve(ctx, repo, number)}
	}
}

func (m Model) handleDiffReviewed(msg diffReviewedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "review failed: " + msg.err.Error()
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

// deleteDiffDraft drops the pending note under the cursor.
func (m Model) deleteDiffDraft() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil || d.cursor >= len(d.rows) || d.rows[d.cursor].draft == 0 {
		m.status = "no pending note here"
		return m, nil
	}
	dr := d.drafts[d.rows[d.cursor].draft-1]
	c, ctx, repo, number, gen := d.c, m.ctx, d.repo, d.number, d.gen
	m.status = "dropping the pending note…"
	return m, func() tea.Msg {
		return diffReviewedMsg{gen: gen, what: "pending note dropped", err: c.DeleteDraft(ctx, repo, number, dr.ID)}
	}
}
