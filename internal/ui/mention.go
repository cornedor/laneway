package ui

import (
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/emoji"
	"github.com/cornedor/laneway/internal/jira"
)

// @mentions in the comment composer: "@" and a few letters before the
// cursor search the issue's assignable people; tab takes the chosen one,
// written as "@Name" and posted as a real mention. ":" and two letters
// offer emoji the same way, from the emoji table, written as ":name:" and
// posted as Jira's emoji.

const (
	mentionDelay = 250 * time.Millisecond
	mentionShown = 5
)

// mentionState is the composer's open completion.
type mentionState struct {
	start int // where the "@" (or ":") is, in runes
	sugg  []jira.User
	emoji []string // the emoji offered instead, their names
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
	err   error
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

// mentionEditor is the editor @ completes in and where its people are
// searched: the comment composer on its issue, or the create form's
// description in its project; nil for none.
func (m *Model) mentionEditor() (*editor.Model, string) {
	if m.jiraCommentActive {
		return &m.jiraCommentInput, m.jiraCommentKey
	}
	if f := m.jiraForm; f != nil && f.create != nil && f.create.form && f.editing && f.multiline && f.idx < len(f.fields) && f.fields[f.idx].ID == createDescField {
		return &f.area, f.create.in.Project
	}
	return nil, ""
}

// scheduleMention arms a search for the name at the cursor, or drops the
// completion when there is none.
func (m *Model) scheduleMention() tea.Cmd {
	ed, _ := m.mentionEditor()
	if ed == nil {
		return nil
	}
	if q, start, ok := emojiQueryAt(ed.Value(), ed.CursorOffset()); ok {
		m.jiraMention = mentionState{seq: m.jiraMention.seq + 1, start: start, emoji: emojiMatches(q)}
		return nil
	}
	q, start, ok := mentionAt(ed.Value(), ed.CursorOffset())
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
	ed, key := m.mentionEditor()
	if ed == nil || msg.seq != m.jiraMention.seq {
		return m, nil
	}
	c, ctx := m.jiraClient, m.ctx
	return m, func() tea.Msg {
		users, err := c.AssignableUsers(ctx, key, msg.query)
		return mentionFoundMsg{msg.seq, users, err}
	}
}

func (m Model) handleMentionFound(msg mentionFoundMsg) (tea.Model, tea.Cmd) {
	if ed, _ := m.mentionEditor(); ed == nil || msg.seq != m.jiraMention.seq {
		return m, nil
	}
	if msg.err != nil {
		m.fail("mention search: " + msg.err.Error())
	}
	m.jiraMention.sugg = msg.users[:min(len(msg.users), mentionShown)]
	m.jiraMention.idx = 0
	return m, nil
}

// mentionKey handles the completion's keys while it shows; false otherwise.
func (m *Model) mentionKey(k string) bool {
	ms := &m.jiraMention
	n := max(len(ms.sugg), len(ms.emoji))
	if n == 0 {
		return false
	}
	switch k {
	case "ctrl+n":
		ms.idx = (ms.idx + 1) % n
	case "ctrl+p":
		ms.idx = (ms.idx + n - 1) % n
	case "tab":
		if len(ms.emoji) > 0 {
			m.acceptEmoji(ms.emoji[ms.idx])
		} else {
			m.acceptMention(ms.sugg[ms.idx])
		}
	default:
		return false
	}
	return true
}

// acceptMention writes "@Name " over the typed "@query" and remembers the
// person, so posting makes it a mention.
func (m *Model) acceptMention(u jira.User) {
	in, _ := m.mentionEditor()
	if in == nil {
		return
	}
	r := []rune(in.Value())
	cur := min(in.CursorOffset(), len(r))
	start := min(m.jiraMention.start, cur)
	name := "@" + u.DisplayName + " "
	in.SetValue(string(r[:start]) + name + string(r[cur:]))
	in.SetCursorOffset(start + len([]rune(name)))
	who := jira.Mention{AccountID: u.AccountID, DisplayName: u.DisplayName}
	if m.jiraCommentActive {
		m.jiraCommentMentions = append(m.jiraCommentMentions, who)
	} else {
		m.jiraForm.create.mentions = append(m.jiraForm.create.mentions, who)
	}
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}

// renderMentions is the completion list under the composer, "" without one.
func (m *Model) renderMentions() string {
	ms := m.jiraMention
	if len(ms.emoji) > 0 {
		var lines []string
		for i, name := range ms.emoji {
			row := emoji.Glyph(name) + " :" + name + ":"
			if i == ms.idx {
				lines = append(lines, lipgloss.NewStyle().Foreground(focusedColor).Bold(true).Render("▸ "+row))
			} else {
				lines = append(lines, "  "+refDimStyle.Render(row))
			}
		}
		return strings.Join(append(lines, refDimStyle.Render("tab inserts · ctrl+n/p choose")), "\n")
	}
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

// emojiQuery matches ":" and two or more name letters ending at the cursor.
var emojiQuery = regexp.MustCompile(`(?:^|\s):([a-z0-9_+\-]{2,})$`)

// emojiQueryAt is the emoji name being typed before the cursor, and where
// its ":" is; ok false when none.
func emojiQueryAt(text string, cursor int) (query string, start int, ok bool) {
	r := []rune(text)
	cursor = min(cursor, len(r))
	m := emojiQuery.FindStringSubmatch(string(r[:cursor]))
	if m == nil {
		return "", 0, false
	}
	return m[1], cursor - len([]rune(m[1])) - 1, true
}

// emojiMatches are the first mentionShown emoji names starting with q,
// then those holding it.
func emojiMatches(q string) []string {
	var pre, in []string
	for _, n := range emoji.Names() {
		switch {
		case strings.HasPrefix(n, q):
			pre = append(pre, n)
		case len(in) < mentionShown && strings.Contains(n, q):
			in = append(in, n)
		}
		if len(pre) == mentionShown {
			break
		}
	}
	return append(pre, in...)[:min(len(pre)+len(in), mentionShown)]
}

// acceptEmoji writes ":name: " over the typed ":query".
func (m *Model) acceptEmoji(name string) {
	in, _ := m.mentionEditor()
	if in == nil {
		return
	}
	r := []rune(in.Value())
	cur := min(in.CursorOffset(), len(r))
	start := min(m.jiraMention.start, cur)
	code := ":" + name + ": "
	in.SetValue(string(r[:start]) + code + string(r[cur:]))
	in.SetCursorOffset(start + len([]rune(code)))
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}
