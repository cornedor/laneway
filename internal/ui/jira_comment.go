package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/jira"
)

// The Jira comment composer: a modal multi-line input for adding a comment to
// the open issue (c) or replying to one (R, via the reply-target picker in
// jira_edit.go). It mirrors the message composer's keys — Enter posts,
// alt/shift+enter inserts a newline, esc cancels — and is fully modal: it owns
// every keystroke while open (dispatched in update.go before the focus-based
// routing) and overlays the screen (view.go), like the field pickers. A
// confirmed post goes through internal/jira, reusing the jiraMutated path so the
// panel refetches and the new comment shows. A reply prefills an editable quote
// of the original and pings its author with a real ADF mention (a plain "@name"
// would not notify).

// commentQuoteMaxLines caps how much of the original comment a reply quotes, so
// a long comment doesn't bury the composer.
const commentQuoteMaxLines = 8

// newCommentTextarea is the shared modal composer, seeded with Jira's
// placeholder. See modalcomposer.go for the box it is drawn in.
func newCommentTextarea() editor.Model {
	return newModalComposer("comment…")
}

// openJiraCommentInput opens an empty composer for a new top-level comment on
// the shown issue.
func (m *Model) openJiraCommentInput() {
	m.jiraCommentActive = true
	m.jiraCommentKey = m.jiraIssue.Key
	m.jiraCommentMention = nil
	m.jiraCommentReplyTo, m.jiraCommentReplyID = "", ""
	m.jiraCommentInput = newCommentTextarea()
	m.jiraCommentBefore, m.jiraCommentDiscard = "", false
	if m.unsent.key == m.jiraCommentKey && m.unsent.text != "" {
		m.jiraCommentInput.SetValue(m.unsent.text)
		m.jiraCommentInput.CursorEnd()
		m.unsent = struct{ key, text string }{}
		m.status = "your unsent comment is back"
	} else if text, at, ok := m.draft(commentDraft(m.jiraCommentKey)); ok {
		m.jiraCommentInput.SetValue(text)
		m.jiraCommentInput.CursorEnd()
		m.status = "your draft from " + draftWhen(at, time.Now()) + " is back · esc twice drops it"
	}
}

// commentInline is whether the composer sits in the panel: under the comment
// it replies to, else after the activity.
func (m *Model) commentInline() bool {
	return m.jiraCommentActive && m.refOpen && m.jiraIssue != nil && m.jiraIssue.Key == m.jiraCommentKey && m.refErr == nil && !m.refLoading
}

// panelComposing is whether an editor is open in the panel's body.
func (m *Model) panelComposing() bool { return m.descEditInline() || m.commentInline() }

// commentMarkLine is an editor's stand-in in the thread (the composer's or
// a comment edit's mark), indented by depth reply bars.
func (m *Model) commentMarkLine(mark string, depth int) string {
	m.commentIndent = depth
	return mark + "\n"
}

// openJiraReply opens the composer prefilled with an editable quote of c and
// arranged to @mention its author, so the post reads as (and notifies like) a
// reply. The user is free to trim the quote or change the text before posting.
func (m *Model) openJiraReply(c jira.Comment) {
	m.jiraCommentActive = true
	m.jiraCommentKey = m.jiraIssue.Key
	m.jiraCommentReplyTo, m.jiraCommentReplyID = c.Author, c.ID
	if c.AuthorID != "" {
		m.jiraCommentMention = &jira.Mention{AccountID: c.AuthorID, DisplayName: c.Author}
	} else {
		m.jiraCommentMention = nil
	}
	ta := newCommentTextarea()
	ta.SetValue(replyQuote(c))
	ta.CursorEnd()
	m.jiraCommentInput = ta
	m.jiraCommentBefore, m.jiraCommentDiscard = ta.Value(), false
}

// replyQuote builds the editable reply seed: a markdown blockquote of c's
// author + body (capped), then a blank line the cursor lands on.
func replyQuote(c jira.Comment) string {
	author := c.Author
	if author == "" {
		author = "comment"
	}
	var b strings.Builder
	b.WriteString("> " + author + " wrote:\n")
	lines := strings.Split(strings.TrimSpace(c.Body), "\n")
	for i, ln := range lines {
		if i >= commentQuoteMaxLines {
			b.WriteString("> …\n")
			break
		}
		b.WriteString("> " + ln + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// closeJiraComment tears the composer down.
func (m *Model) closeJiraComment() {
	m.jiraCommentActive = false
	m.jiraCommentVis = jira.Visibility{}
	m.jiraCommentKey = ""
	m.jiraCommentMention = nil
	m.jiraCommentReplyTo, m.jiraCommentReplyID = "", ""
	m.jiraCommentInput = editor.Model{}
	m.jiraCommentBefore, m.jiraCommentDiscard = "", false
	m.jiraCommentMentions = nil
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}

// handleJiraCommentKey owns every keystroke while the composer is open: esc
// cancels (asking once when you wrote something), Enter or ctrl+s posts,
// alt/shift+enter insert a newline (bound on the textarea), everything else
// edits the text.
func (m Model) handleJiraCommentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		if strings.TrimSpace(m.jiraCommentInput.Value()) != strings.TrimSpace(m.jiraCommentBefore) && !m.jiraCommentDiscard {
			m.jiraCommentDiscard = true
			m.status = "esc again discards your comment · enter posts it"
			return m, nil
		}
		if m.jiraCommentReplyTo == "" {
			m.dropDraft(commentDraft(m.jiraCommentKey))
		}
		m.closeJiraComment()
		m.status = ""
		return m, nil
	case "enter", "ctrl+s":
		return m.applyJiraComment()
	case "ctrl+o":
		return m, m.cycleCommentVis()
	}
	m.jiraCommentDiscard = false
	if m.mentionKey(msg.String()) {
		return m, nil
	}
	var cmd tea.Cmd
	m.jiraCommentInput, cmd = m.jiraCommentInput.Update(msg)
	return m, tea.Batch(cmd, m.scheduleMention(), m.scheduleDraftSave())
}

// applyJiraComment closes the composer and posts the comment (or reply). An
// empty body with no mention is treated as a cancel.
func (m Model) applyJiraComment() (tea.Model, tea.Cmd) {
	key := m.jiraCommentKey
	text := strings.TrimSpace(m.jiraCommentInput.Value())
	mention, inline, vis := m.jiraCommentMention, m.jiraCommentMentions, m.jiraCommentVis
	m.saveOpenDrafts() // until Jira has it
	m.closeJiraComment()
	if text == "" && mention == nil {
		return m, nil
	}
	client, ctx := m.jiraClient, m.ctx
	verb := "comment on"
	if mention != nil {
		verb = "reply to"
	}
	m.status = fmt.Sprintf("posting %s %s…", verb, key)
	return m, func() tea.Msg {
		err := client.AddCommentMentions(ctx, key, text, mention, inline, vis)
		return jiraMutatedMsg{key: key, field: "comment", err: err, text: text}
	}
}

// renderJiraCommentInput draws the modal composer, with a "replying to" line in
// reply mode. The box itself is the shared one (modalcomposer.go) — the diff
// view's inline note uses the same frame.
func (m *Model) renderJiraCommentInput() string {
	if !m.jiraCommentActive {
		return ""
	}
	titleTxt := "Comment — " + m.jiraCommentKey
	var above []string
	if m.jiraCommentReplyTo != "" {
		titleTxt = "Reply — " + m.jiraCommentKey
	}
	if v := m.jiraCommentVis; v != (jira.Visibility{}) {
		titleTxt += " · " + v.Label()
	}
	if m.jiraCommentReplyTo != "" {
		above = append(above, lipgloss.NewStyle().Foreground(dimColor).Italic(true).
			Render("↩ replying to "+m.jiraCommentReplyTo))
	}
	box := m.renderModalComposer(titleTxt, above, "↵ post · alt+↵ newline · @ mention · ctrl+o who sees it · esc cancel", &m.jiraCommentInput)
	if list := m.renderMentions(); list != "" {
		box = lipgloss.JoinVertical(lipgloss.Left, box, list)
	}
	return box
}

// commentVisMsg is who a comment in project can be limited to.
type commentVisMsg struct {
	project string
	vis     []jira.Visibility
	err     error
}

// cycleCommentVis steps who the comment is for: everyone, an internal
// note (Service Desk), each project role. The first press asks Jira.
func (m *Model) cycleCommentVis() tea.Cmd {
	project := issueProject(m.jiraCommentKey)
	opts, ok := m.commentVis[project]
	if !ok {
		m.status = "asking who a comment can be for…"
		c, ctx := m.jiraClient, m.ctx
		return func() tea.Msg {
			vis, err := c.CommentVisibilities(ctx, project)
			return commentVisMsg{project, vis, err}
		}
	}
	all := append([]jira.Visibility{{}}, opts...)
	i := slices.Index(all, m.jiraCommentVis)
	m.jiraCommentVis = all[(i+1)%len(all)]
	m.status = "the comment is for " + m.jiraCommentVis.Label()
	return nil
}

func (m Model) handleCommentVis(msg commentVisMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail("who can see comments: " + msg.err.Error())
		return m, nil
	}
	if m.commentVis == nil {
		m.commentVis = map[string][]jira.Visibility{}
	}
	m.commentVis[msg.project] = msg.vis
	if len(msg.vis) == 0 {
		m.status = "comments in " + msg.project + " are for everyone: no roles to limit them to"
		return m, nil
	}
	if m.jiraCommentActive && issueProject(m.jiraCommentKey) == msg.project {
		return m, m.cycleCommentVis()
	}
	return m, nil
}

// deleteComment deletes comment i of the panel issue; u posts it again.
func (m *Model) deleteComment(i int) tea.Cmd {
	key, id, c, ctx := m.jiraIssue.Key, m.jiraIssue.Comments[i].ID, m.jiraClient, m.ctx
	m.undoDeleteComment(key, m.jiraIssue.Comments[i])
	m.status = "deleting the comment…"
	return jiraMutateCmd(key, "comment deleted", func() error { return c.DeleteComment(ctx, key, id) })
}

// clickCommentAction does the action at column col of comment i's action
// row: reply, edit, or delete, which a second click confirms.
func (m Model) clickCommentAction(i, col int) (tea.Model, tea.Cmd) {
	if m.jiraIssue == nil || i < 0 || i >= len(m.jiraIssue.Comments) {
		return m, nil
	}
	c := m.jiraIssue.Comments[i]
	act := m.commentActionAt(c, col)
	confirm := m.commentDelete == c.ID
	m.commentDelete = ""
	switch act {
	case "reply":
		m.openJiraReply(c)
	case "edit":
		return m, m.editComment(i)
	case "delete":
		if confirm {
			return m, m.deleteComment(i)
		}
		m.commentDelete = c.ID
		m.status = "click delete again to delete the comment"
	}
	m.renderRef()
	return m, nil
}
