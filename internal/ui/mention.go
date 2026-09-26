package ui

import (
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// @mentions in the comment composer: "@" and a few letters before the
// cursor search the issue's assignable people; tab takes the chosen one,
// written as "@Name" and posted as a real mention.

const (
	mentionDelay = 250 * time.Millisecond
	mentionShown = 5
)

// mentionState is the composer's open completion.
type mentionState struct {
	start int // where the "@" is, in runes
	sugg  []jira.User
	idx   int
	seq   int
}

type mentionSearchMsg struct {
	seq   int
	query string
}

type mentionFoundMsg struct {
	seq   int
	users []jira.User
}

// mentionQuery matches "@" and a name start ending at the cursor.
var mentionQuery = regexp.MustCompile(`(?:^|\s)@([\pL][\pL\pN.\-]*)$`)

// mentionAt is the name being typed before the cursor, and where its "@"
// is; ok false when none.
func mentionAt(text string, cursor int) (query string, start int, ok bool) {
	r := []rune(text)
	if cursor > len(r) {
		cursor = len(r)
	}
	before := string(r[:cursor])
	m := mentionQuery.FindStringSubmatch(before)
	if m == nil {
		return "", 0, false
	}
	return m[1], cursor - len([]rune(m[1])) - 1, true
}

// scheduleMention arms a search for the name at the cursor, or drops the
// completion when there is none.
func (m *Model) scheduleMention() tea.Cmd {
	q, start, ok := mentionAt(m.jiraCommentInput.Value(), m.jiraCommentInput.CursorOffset())
	if !ok {
		m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
		return nil
	}
	m.jiraMention.seq++
	m.jiraMention.start = start
	seq := m.jiraMention.seq
	return tea.Tick(mentionDelay, func(time.Time) tea.Msg { return mentionSearchMsg{seq, q} })
}

func (m Model) handleMentionSearch(msg mentionSearchMsg) (tea.Model, tea.Cmd) {
	if !m.jiraCommentActive || msg.seq != m.jiraMention.seq {
		return m, nil
	}
	c, ctx, key := m.jiraClient, m.ctx, m.jiraCommentKey
	return m, func() tea.Msg {
		users, _ := c.AssignableUsers(ctx, key, msg.query)
		return mentionFoundMsg{msg.seq, users}
	}
}

func (m Model) handleMentionFound(msg mentionFoundMsg) (tea.Model, tea.Cmd) {
	if !m.jiraCommentActive || msg.seq != m.jiraMention.seq {
		return m, nil
	}
	m.jiraMention.sugg = msg.users[:min(len(msg.users), mentionShown)]
	m.jiraMention.idx = 0
	return m, nil
}

// mentionKey handles the completion's keys while it shows; false otherwise.
func (m *Model) mentionKey(k string) bool {
	ms := &m.jiraMention
	if len(ms.sugg) == 0 {
		return false
	}
	switch k {
	case "ctrl+n":
		ms.idx = (ms.idx + 1) % len(ms.sugg)
	case "ctrl+p":
		ms.idx = (ms.idx + len(ms.sugg) - 1) % len(ms.sugg)
	case "tab":
		m.acceptMention(ms.sugg[ms.idx])
	default:
		return false
	}
	return true
}

// acceptMention writes "@Name " over the typed "@query" and remembers the
// person, so posting makes it a mention.
func (m *Model) acceptMention(u jira.User) {
	in := &m.jiraCommentInput
	r := []rune(in.Value())
	cur := min(in.CursorOffset(), len(r))
	start := min(m.jiraMention.start, cur)
	name := "@" + u.DisplayName + " "
	in.SetValue(string(r[:start]) + name + string(r[cur:]))
	in.SetCursorOffset(start + len([]rune(name)))
	m.jiraCommentMentions = append(m.jiraCommentMentions, jira.Mention{AccountID: u.AccountID, DisplayName: u.DisplayName})
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}

// renderMentions is the completion list under the composer, "" without one.
func (m *Model) renderMentions() string {
	ms := m.jiraMention
	if len(ms.sugg) == 0 {
		return ""
	}
	var lines []string
	for i, u := range ms.sugg {
		if i == ms.idx {
			lines = append(lines, lipgloss.NewStyle().Foreground(focusedColor).Bold(true).Render("▸ @"+u.DisplayName))
		} else {
			lines = append(lines, "  "+refDimStyle.Render("@"+u.DisplayName))
		}
	}
	lines = append(lines, refDimStyle.Render("tab mention · ctrl+n/p choose"))
	return strings.Join(lines, "\n")
}
