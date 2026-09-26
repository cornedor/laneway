package ui

import (
	"fmt"
	"strings"

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
	m.jiraCommentKey = ""
	m.jiraCommentMention = nil
	m.jiraCommentReplyTo, m.jiraCommentReplyID = "", ""
	m.jiraCommentInput = editor.Model{}
	m.jiraCommentMentions = nil
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}

// handleJiraCommentKey owns every keystroke while the composer is open: esc
// cancels, Enter posts, alt/shift+enter insert a newline (bound on the
// textarea), everything else edits the text.
func (m Model) handleJiraCommentKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.closeJiraComment()
		return m, nil
	case "enter":
		return m.applyJiraComment()
	}
	if m.mentionKey(msg.String()) {
		return m, nil
	}
	var cmd tea.Cmd
	m.jiraCommentInput, cmd = m.jiraCommentInput.Update(msg)
	return m, tea.Batch(cmd, m.scheduleMention())
}

// applyJiraComment closes the composer and posts the comment (or reply). An
// empty body with no mention is treated as a cancel.
func (m Model) applyJiraComment() (tea.Model, tea.Cmd) {
	key := m.jiraCommentKey
	text := strings.TrimSpace(m.jiraCommentInput.Value())
	mention, inline := m.jiraCommentMention, m.jiraCommentMentions
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
	return m, jiraMutateCmd(key, "comment", func() error {
		return client.AddCommentMentions(ctx, key, text, mention, inline)
	})
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
		above = append(above, lipgloss.NewStyle().Foreground(dimColor).Italic(true).
			Render("↩ replying to "+m.jiraCommentReplyTo))
	}
	box := m.renderModalComposer(titleTxt, above, "↵ post · alt+↵ newline · @ mention · esc cancel", &m.jiraCommentInput)
	if list := m.renderMentions(); list != "" {
		box = lipgloss.JoinVertical(lipgloss.Left, box, list)
	}
	return box
}
