package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/i18n"
)

// Selecting text in the panel: a drag over its body selects from where the
// press was to the pointer, and letting go copies it (OSC 52, as the other
// copies). The terminal's own selection would take the board beside it too.

// panelSel is the panel's text selection: from a to b, each a content line
// and a column in it. held while the button is down; on once it spans
// something.
type panelSel struct {
	held, on bool
	a, b     [2]int
}

var panelSelStyle = lipgloss.NewStyle().Reverse(true)

// panelPos is the content line and column under screen cell x, y of the
// panel body; ok false off it.
func (m *Model) panelPos(x, y int) ([2]int, bool) {
	i, wrap := m.panelCellAt(y)
	if i < 0 {
		return [2]int{}, false
	}
	listW, _ := m.jiraListWidth(m.width)
	return [2]int{i, max(x-listW-1, 0) + wrap*m.refView.Width()}, true
}

// startPanelSel arms a selection at the press at x, y, dropping the one
// shown.
func (m *Model) startPanelSel(x, y int) {
	shown := m.panelSel.on
	m.panelSel = panelSel{}
	if p, ok := m.panelPos(x, y); ok {
		m.panelSel = panelSel{held: true, a: p, b: p}
	}
	if shown {
		m.renderRef()
	}
}

// dragPanelSel moves the selection's end to the pointer; above or below the
// body it scrolls a line.
func (m Model) dragPanelSel(x, y int) (tea.Model, tea.Cmd) {
	top := 1 + m.crumbRows()
	switch {
	case y < top:
		m.refView.ScrollUp(1)
		y = top
	case y >= top+m.refView.Height():
		m.refView.ScrollDown(1)
		y = top + m.refView.Height() - 1
	}
	p, ok := m.panelPos(x, y)
	if !ok || p == m.panelSel.b && m.panelSel.on {
		return m, nil
	}
	m.panelSel.b = p
	m.panelSel.on = p != m.panelSel.a
	m.renderRef()
	return m, nil
}

// endPanelSel lets go of the button: a selection is copied.
func (m Model) endPanelSel() (tea.Model, tea.Cmd) {
	m.panelSel.held = false
	if !m.panelSel.on {
		return m, nil
	}
	text := m.panelSelText()
	if text == "" {
		return m, nil
	}
	m.status = fmt.Sprintf(i18n.T("copied %d characters"), len([]rune(text)))
	return m, tea.SetClipboard(text)
}

// panelSelRange is the selection in reading order: its first line and
// column, its last line and the column after its last character.
func (m *Model) panelSelRange() (from, to [2]int) {
	a, b := m.panelSel.a, m.panelSel.b
	if b[0] < a[0] || b[0] == a[0] && b[1] < a[1] {
		a, b = b, a
	}
	b[1]++
	return a, b
}

// panelSelSpan is the columns of line i the selection covers, ok false
// when it covers none of it.
func (m *Model) panelSelSpan(i, width int) (from, to int, ok bool) {
	s, e := m.panelSelRange()
	if !m.panelSel.on || i < s[0] || i > e[0] {
		return 0, 0, false
	}
	from, to = 0, width
	if i == s[0] {
		from = s[1]
	}
	if i == e[0] {
		to = min(e[1], width)
	}
	return from, to, from < to
}

// markPanelSel draws the selection over content's lines.
func (m *Model) markPanelSel(content string) string {
	if !m.panelSel.on {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		w := ansi.StringWidth(l)
		if from, to, ok := m.panelSelSpan(i, w); ok {
			lines[i] = ansi.Cut(l, 0, from) + panelSelStyle.Render(ansi.Strip(ansi.Cut(l, from, to))) + ansi.Cut(l, to, w)
		}
	}
	return strings.Join(lines, "\n")
}

// panelSelText is the selected text, unstyled: each line's trailing space
// trimmed and the indent the lines after the first share taken off (the
// first starts where the drag did).
func (m *Model) panelSelText() string {
	var lines []string
	var at []int // each line's panel row
	for i, l := range m.panelPlain {
		if from, to, ok := m.panelSelSpan(i, ansi.StringWidth(l)); ok {
			lines, at = append(lines, strings.TrimRight(ansi.Cut(l, from, to), " ")), append(at, i)
		} else if s, e := m.panelSelRange(); i > s[0] && i < e[0] {
			lines, at = append(lines, ""), append(at, i)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	indent := -1
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) != "" {
			n := len(l) - len(strings.TrimLeft(l, " "))
			if indent < 0 || n < indent {
				indent = n
			}
		}
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			l = l[min(max(indent, 0), len(l)):]
			if r := at[i-1]; r < len(m.panelSoft) && m.panelSoft[r] && at[i] == r+1 {
				// A wrapped row: the words run on.
				b.WriteString(" ")
				l = strings.TrimLeft(l, " ")
			} else {
				b.WriteString("\n")
			}
		}
		b.WriteString(l)
	}
	return strings.TrimSpace(b.String())
}
