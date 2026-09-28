package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/emoji"
)

// ":" completion, after matterbox's: ":" at a word's start and two or more
// shortcode letters before the cursor list the emoji that match, best
// first — exact, then prefix, then anywhere in the name, then the letters
// in order (":smle" finds :smile:) — and within each, the ones you took
// most. Emoticons (":)", ":-)") never open it.

// emojiShown caps the list.
const emojiShown = 8

// emojiUsageMeta is the store key of how often each emoji was taken.
const emojiUsageMeta = "emoji_usage"

// emojiEditor is the editor ":" completes in: the mention editors, or the
// description editor; nil for none.
func (m *Model) emojiEditor() *editor.Model {
	if ed, _ := m.mentionEditor(); ed != nil {
		return ed
	}
	if m.descEdit != nil {
		return &m.descEdit.input
	}
	return nil
}

// scheduleEmoji opens, updates or closes the emoji list for the text before
// the cursor; true while it shows.
func (m *Model) scheduleEmoji() bool {
	ed := m.emojiEditor()
	if ed == nil {
		return false
	}
	q, start, ok := emojiQueryAt(ed.Value(), ed.CursorOffset())
	if !ok {
		if len(m.jiraMention.emoji) > 0 {
			m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
		}
		return false
	}
	if ms := m.jiraMention; len(ms.emoji) > 0 && ms.start == start && ms.query == q {
		return true // the same query: keep the choice
	}
	items := m.emojiMatches(q)
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1, start: start, emoji: items, query: q}
	return len(items) > 0
}

// emojiQueryAt is the emoji name being typed before the cursor, lower
// case, and where its ":" is; ok false when none: no ":" at a word's
// start, fewer than two letters, or an emoticon's punctuation.
func emojiQueryAt(text string, cursor int) (query string, start int, ok bool) {
	r := []rune(text)
	cursor = min(cursor, len(r))
	at := -1
	for i := cursor - 1; i >= 0; i-- {
		if r[i] == ':' {
			if i == 0 || unicode.IsSpace(r[i-1]) {
				at = i
			}
			break
		}
		if unicode.IsSpace(r[i]) {
			break
		}
	}
	if at < 0 || cursor-at-1 < 2 {
		return "", 0, false
	}
	q := strings.ToLower(string(r[at+1 : cursor]))
	if strings.IndexFunc(q, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '+' || c == '-')
	}) >= 0 {
		return "", 0, false
	}
	return q, at, true
}

// emojiMatches are the best emojiShown names for q.
func (m *Model) emojiMatches(q string) []string {
	type cand struct {
		name        string
		band, score int
	}
	var cands []cand
	for _, n := range emoji.Names() {
		if band, score, ok := fuzzyScore(n, q); ok {
			cands = append(cands, cand{n, band, score})
		}
	}
	use := m.emojiUsage()
	slices.SortStableFunc(cands, func(a, b cand) int {
		switch {
		case a.band != b.band:
			return a.band - b.band
		case use[a.name] != use[b.name]:
			return use[b.name] - use[a.name]
		case a.score != b.score:
			return a.score - b.score
		}
		return strings.Compare(a.name, b.name)
	})
	out := make([]string, 0, min(len(cands), emojiShown))
	for _, c := range cands[:min(len(cands), emojiShown)] {
		out = append(out, c.name)
	}
	return out
}

// fuzzyScore ranks needle in haystack: band 0 exact, 1 prefix, 2 inside,
// 3 its letters in order; score orders within a band, lower first.
func fuzzyScore(haystack, needle string) (band, score int, ok bool) {
	if i := strings.Index(haystack, needle); i >= 0 {
		switch {
		case len(haystack) == len(needle):
			band = 0
		case i == 0:
			band = 1
		default:
			band = 2
		}
		return band, i*2 + len(haystack) - len(needle), true
	}
	hi, gaps := 0, 0
	for _, c := range []byte(needle) {
		for hi < len(haystack) && haystack[hi] != c {
			hi, gaps = hi+1, gaps+1
		}
		if hi >= len(haystack) {
			return 0, 0, false
		}
		hi++
	}
	return 3, gaps, true
}

// emojiUsage is how often each emoji was taken, from the store.
func (m *Model) emojiUsage() map[string]int {
	use := map[string]int{}
	if m.store != nil {
		if v, ok, _ := m.store.GetMeta(emojiUsageMeta); ok {
			_ = json.Unmarshal([]byte(v), &use)
		}
	}
	return use
}

// acceptEmoji writes ":name: " over the typed ":query", and counts it.
func (m *Model) acceptEmoji(name string) {
	in := m.emojiEditor()
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
	if m.store != nil {
		use := m.emojiUsage()
		use[name]++
		if b, err := json.Marshal(use); err == nil {
			_ = m.store.SetMeta(emojiUsageMeta, string(b))
		}
	}
}

// renderEmojiList is the emoji list: a box of each one's glyph and code,
// the chosen one lit.
func (m *Model) renderEmojiList() string {
	ms := m.jiraMention
	rows := make([]string, 0, len(ms.emoji)+1)
	for i, name := range ms.emoji {
		row := emoji.Glyph(name) + "  :" + name + ":"
		if i == ms.idx {
			rows = append(rows, selectedRow.Render(row))
		} else {
			rows = append(rows, emoji.Glyph(name)+"  "+refDimStyle.Render(":"+name+":"))
		}
	}
	rows = append(rows, refDimStyle.Render("tab takes · ↑/↓ · esc"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(0, 1).
		Render(strings.Join(rows, "\n"))
}
