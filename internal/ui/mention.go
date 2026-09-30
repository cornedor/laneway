package ui

import (
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/emoji"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/textwidth"
)

// @mentions in the comment composer: "@" and a few letters before the
// cursor search the issue's assignable people, from the project's kept
// list at once, else Jira after mentionDelay; tab takes the chosen one,
// written as "@Name" and posted as a real mention; in the description
// editor too, where the person is kept to save it as one. ":" and two
// letters offer emoji the same way (emoji_complete.go), written as
// ":name:" and posted as Jira's emoji; "/" at a line's start Jira's
// content (quick_insert.go). While a list shows,
// ↑/↓ (ctrl+p/n) choose, tab or enter takes, esc closes it. The list is a
// box drawn over the screen at the cursor, so nothing under it moves.

const (
	mentionDelay = 250 * time.Millisecond
	mentionShown = 5
)

// mentionState is the composer's open completion.
type mentionState struct {
	start int // where the "@" (or ":") is, in runes
	sugg  []jira.User
	emoji []string    // the emoji offered instead, their names, for query
	quick []quickItem // or what "/" inserts
	query string
	idx   int
	seq   int
	// searching is a Jira search on its way, the people it can't find
	// kept narrowed to query meanwhile.
	searching bool
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
// searched: the comment composer on its issue, the description (or a
// comment or field) being edited on its issue, or the create form's
// description in its project; nil for none.
func (m *Model) mentionEditor() (*editor.Model, string) {
	if m.jiraCommentActive {
		return &m.jiraCommentInput, m.jiraCommentKey
	}
	if m.descEdit != nil {
		return &m.descEdit.input, m.descEdit.key
	}
	if f := m.jiraForm; f != nil && f.create != nil && f.create.form && f.editing && f.multiline && f.idx < len(f.fields) && f.fields[f.idx].ID == createDescField {
		return &f.area, f.create.in.Project
	}
	return nil, ""
}

// scheduleMention arms a search for the name at the cursor, or drops the
// completion when there is none.
func (m *Model) scheduleMention() tea.Cmd {
	if m.scheduleQuick() {
		return nil
	}
	ed, _ := m.mentionEditor()
	if ed == nil {
		return nil
	}
	if m.scheduleEmoji() {
		return nil
	}
	q, start, ok := mentionAt(ed.Value(), ed.CursorOffset())
	if !ok {
		m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
		return nil
	}
	_, key := m.mentionEditor()
	ms := &m.jiraMention
	ms.seq++
	ms.start, ms.query = start, q
	if us, ok := m.jiraClient.KnownUsers(key, q); ok { // the project's people, kept: at once
		ms.searching = false
		m.setMentionSugg(us)
		return nil
	}
	var still []jira.User // what shows narrowed, until Jira answers
	for _, u := range ms.sugg {
		if nameMatches(u.DisplayName, q) {
			still = append(still, u)
		}
	}
	m.setMentionSugg(still)
	ms.searching = true
	seq := ms.seq
	return tea.Tick(mentionDelay, func(time.Time) tea.Msg { return mentionSearchMsg{seq, q} })
}

// setMentionSugg shows us, the choice kept on the same person when they
// are still there.
func (m *Model) setMentionSugg(us []jira.User) {
	ms := &m.jiraMention
	chosen := ""
	if ms.idx < len(ms.sugg) {
		chosen = ms.sugg[ms.idx].AccountID
	}
	ms.sugg = us[:min(len(us), mentionShown)]
	ms.idx = max(slices.IndexFunc(ms.sugg, func(u jira.User) bool { return u.AccountID == chosen }), 0)
}

// nameMatches is whether each word of q starts a word of name, as Jira
// matches people.
func nameMatches(name, q string) bool {
	words := strings.Fields(strings.ToLower(name))
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !slices.ContainsFunc(words, func(n string) bool { return strings.HasPrefix(n, w) }) {
			return false
		}
	}
	return true
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
	m.jiraMention.searching = false
	m.setMentionSugg(msg.users)
	return m, nil
}

// mentionKey handles the completion's keys while it shows; false otherwise.
func (m *Model) mentionKey(k string) bool {
	ms := &m.jiraMention
	n := max(len(ms.sugg), len(ms.emoji), len(ms.quick))
	if n == 0 {
		if ms.searching && k == "esc" { // "searching…" shows
			m.jiraMention = mentionState{seq: ms.seq + 1}
			return true
		}
		return false
	}
	switch k {
	case "down", "ctrl+n":
		ms.idx = (ms.idx + 1) % n
	case "up", "ctrl+p":
		ms.idx = (ms.idx + n - 1) % n
	case "tab", "enter":
		switch {
		case len(ms.quick) > 0:
			m.acceptQuick(ms.quick[ms.idx])
			return true
		case len(ms.emoji) > 0:
			m.acceptEmoji(ms.emoji[ms.idx])
		default:
			m.acceptMention(ms.sugg[ms.idx])
		}
	case "esc":
		m.jiraMention = mentionState{seq: ms.seq + 1}
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
	switch {
	case m.jiraCommentActive:
		m.jiraCommentMentions = append(m.jiraCommentMentions, who)
	case m.descEdit != nil: // kept with the markdown, saved as a mention
		m.descEdit.kept = append(m.descEdit.kept, jira.MentionNode("mention", map[string]any{"id": u.AccountID, "text": "@" + u.DisplayName}))
	default:
		m.jiraForm.create.mentions = append(m.jiraForm.create.mentions, who)
	}
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}

// completionWidth is the completion box's inside width: one whatever the
// matches, so it keeps still while you type.
const completionWidth = 30

// renderCompletion is the open completion, people or emoji, as the box
// drawn at the cursor (placeCompletion); "" without one.
func (m *Model) renderCompletion() string {
	ms := m.jiraMention
	var rows []string
	switch {
	case len(ms.quick) > 0:
		for _, it := range ms.quick {
			rows = append(rows, it.label)
		}
	case len(ms.emoji) > 0:
		for _, name := range ms.emoji {
			g := emoji.Glyph(name)
			rows = append(rows, g+strings.Repeat(" ", max(2-textwidth.Width(g), 0))+" :"+name+":") // the names in one column
		}
	case len(ms.sugg) > 0:
		for _, u := range ms.sugg {
			rows = append(rows, "@"+u.DisplayName)
		}
	case ms.searching:
		return completionBox([]string{refDimStyle.Render("searching…")})
	default:
		return ""
	}
	w := min(completionWidth, max(m.width-4, 8))
	for i, r := range rows {
		r = ansi.Truncate(r, w, "…")
		r += strings.Repeat(" ", max(w-textwidth.Width(r), 0))
		if i == ms.idx {
			r = selectedRow.Render(r)
		}
		rows[i] = r
	}
	rows = append(rows, refDimStyle.Render("tab takes · ↑/↓ · esc"))
	return completionBox(rows)
}

// placeCompletion draws the completion box over body at the cursor, its
// text under what you typed: below the line, above it when there's no
// room. ov is the modal drawn, for the create form's place.
func (m *Model) placeCompletion(body, ov string) string {
	box := m.renderCompletion()
	if box == "" {
		return body
	}
	x, y, ok := m.completionCursor(ov)
	if !ok {
		return body
	}
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	left := x - 1 - textwidth.Width(m.jiraMention.query) - 2 // the "@" or ":", the border and padding
	top := y + 1
	if top+h > m.bodyH() {
		top = y - h
	}
	return placeBox(body, box, max(top, 0), max(min(left, m.width-w), 0))
}

// completionCursor is the cursor's cell on screen in the editor completing.
func (m *Model) completionCursor(ov string) (x, y int, ok bool) {
	ed := m.emojiEditor()
	switch {
	case ed == nil:
		return 0, 0, false
	case m.jiraCommentActive:
		if m.commentInline() {
			return m.inlineEditorCursor()
		}
		above := 0
		if m.jiraCommentReplyTo != "" {
			above = 1
		}
		return m.modalComposerCursor(above, ed, ov)
	case m.descEdit != nil && ed == &m.descEdit.input:
		if m.descEditInline() {
			return m.inlineEditorCursor()
		}
		return m.modalComposerCursor(0, ed, ov)
	case m.formArea != nil && ov != "":
		cx, cy, ok := ed.CursorViewPos()
		top, left := placeOffset(m.bodyH(), lipgloss.Height(ov)), placeOffset(m.width, lipgloss.Width(ov))
		return left + m.formArea.x + cx, top + m.formArea.y + cy, ok
	}
	return 0, 0, false
}

func completionBox(rows []string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(0, 1).
		Render(strings.Join(rows, "\n"))
}
