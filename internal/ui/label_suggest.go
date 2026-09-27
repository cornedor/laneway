package ui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// Labels complete as you type, in every labels input (panel l, quick edit,
// bulk B, the create form's Labels): Jira's labels starting with the word
// before the cursor list under it, ↑ ↓ choose, tab takes one. Labels the
// input has already are left out.

// labelSuggest is the list under the active labels input.
type labelSuggest struct {
	list []string
	idx  int
	seq  int // the latest lookup; older answers are dropped
}

const (
	labelSuggestDelay = 150 * time.Millisecond
	labelSuggestMax   = 6
)

// labelInput is the labels input being typed in, nil for none.
func (m *Model) labelInput() *textinput.Model {
	switch f := m.jiraForm; {
	case m.jiraFieldActive && (m.jiraFieldName == "labels" || m.jiraFieldName == "bulk-labels"):
		return &m.jiraFieldInput
	case f != nil && f.editing && !f.multiline && f.idx < len(f.fields) && f.fields[f.idx].ID == "labels":
		return &f.input
	}
	return nil
}

// labelWord is the word before the cursor, split from a bulk edit's + or -
// in front of it; start is where the word begins.
func labelWord(ti *textinput.Model) (sign, word string, start int) {
	r := []rune(ti.Value())
	end := min(ti.Position(), len(r))
	start = end
	for start > 0 && r[start-1] != ' ' {
		start--
	}
	word = string(r[start:end])
	if strings.HasPrefix(word, "+") || strings.HasPrefix(word, "-") {
		return word[:1], word[1:], start
	}
	return "", word, start
}

type labelTickMsg struct{ seq int }

type labelsFoundMsg struct {
	seq   int
	words []string
}

// suggestLabels looks up the word before the cursor once typing pauses.
func (m *Model) suggestLabels() tea.Cmd {
	ti := m.labelInput()
	m.labels.seq++
	m.labels.list = nil
	if ti == nil {
		return nil
	}
	if _, word, _ := labelWord(ti); word == "" {
		return nil
	}
	seq := m.labels.seq
	return tea.Tick(labelSuggestDelay, func(time.Time) tea.Msg { return labelTickMsg{seq} })
}

func (m Model) handleLabelTick(msg labelTickMsg) (tea.Model, tea.Cmd) {
	ti := m.labelInput()
	if ti == nil || msg.seq != m.labels.seq {
		return m, nil
	}
	_, word, _ := labelWord(ti)
	c, ctx := m.jiraClient, m.ctx
	return m, func() tea.Msg {
		words, _ := c.JQLValues(ctx, "labels", word) // none on a failure
		return labelsFoundMsg{seq: msg.seq, words: words}
	}
}

func (m Model) handleLabelsFound(msg labelsFoundMsg) (tea.Model, tea.Cmd) {
	ti := m.labelInput()
	if ti == nil || msg.seq != m.labels.seq {
		return m, nil
	}
	have := map[string]bool{}
	for _, w := range strings.Fields(ti.Value()) {
		have[strings.TrimLeft(w, "+-")] = true
	}
	m.labels.list, m.labels.idx = nil, 0
	for _, w := range msg.words {
		if !have[w] && len(m.labels.list) < labelSuggestMax {
			m.labels.list = append(m.labels.list, w)
		}
	}
	m.renderRef()
	return m, nil
}

// labelKey handles ↑ ↓ and tab while suggestions show; ok is false for any
// other key.
func (m *Model) labelKey(msg tea.KeyPressMsg) bool {
	ls := &m.labels
	if len(ls.list) == 0 {
		return false
	}
	switch msg.String() {
	case "down":
		ls.idx = (ls.idx + 1) % len(ls.list)
	case "up":
		ls.idx = (ls.idx - 1 + len(ls.list)) % len(ls.list)
	case "tab":
		ti := m.labelInput()
		sign, word, start := labelWord(ti)
		r := []rune(ti.Value())
		end := start + len([]rune(sign+word))
		rest := strings.TrimLeft(string(r[end:]), " ")
		ti.SetValue(string(r[:start]) + sign + ls.list[ls.idx] + " " + rest)
		ti.SetCursor(len([]rune(string(r[:start]) + sign + ls.list[ls.idx] + " ")))
		ls.list = nil
	default:
		return false
	}
	m.renderRef()
	return true
}

// labelLines are the suggestions as lines indented by indent, the chosen
// one marked.
func (m *Model) labelLines(indent int) []string {
	if m.labelInput() == nil {
		return nil
	}
	pad := strings.Repeat(" ", indent)
	out := make([]string, 0, len(m.labels.list))
	for i, w := range m.labels.list {
		if i == m.labels.idx {
			out = append(out, pad+jiraKeyStyle.Render("▸ "+w))
		} else {
			out = append(out, pad+refDimStyle.Render("  "+w))
		}
	}
	return out
}
