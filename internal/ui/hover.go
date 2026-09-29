package ui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Hover shows what a click would do before it is made: the target under
// the mouse is underlined, and the pointer turns to a hand over it (OSC 22,
// where the terminal has it). hoverAt follows handleClick's order and asks
// the same hit tests, so what lights up is what a click acts on.

// hover is the target under the mouse: row y, columns x0 to x1 underlined
// (none when x1 <= x0), and the pointer's shape ("" for the default).
type hover struct {
	y, x0, x1 int
	pointer   string
}

const pointerHand, pointerResize = "pointer", "ew-resize"

// hoverAt is the target a click at x, y would act on.
func (m *Model) hoverAt(x, y int) hover {
	listW, _ := m.jiraListWidth(m.width)
	panelRow := hover{y: y, x0: listW + 1, x1: m.width - 1, pointer: pointerHand}
	if m.pickerInline() {
		if x > listW && m.pickerLine >= 0 {
			start, end := m.pickerWindow(inlinePickerRows)
			if i := m.pickerStart + m.panelLineAt(y) - m.pickerLine; m.pickerStart == start && i >= start && i < end {
				return panelRow
			}
		}
		return hover{}
	}
	if m.pickerOnTop() {
		if i, _ := m.pickerRowAt(x, y); i >= 0 && m.jiraPicker.at != nil {
			return hover{pointer: pointerHand} // the row under it is the selected one
		} else if i >= 0 {
			box := m.renderJiraPicker(m.bodyH())
			_, left, _ := m.overlayAt(box, x, y)
			return hover{y: y, x0: left + overlayPadLeft, x1: left + lipgloss.Width(box) - overlayPadLeft, pointer: pointerHand}
		}
		return hover{}
	}
	if y >= m.bodyH() || m.modalOpen() {
		return hover{}
	}
	switch {
	case m.refOpen && x == listW:
		return hover{pointer: pointerResize}
	case m.refOpen && !m.agentTermShown() && x == m.width-1 && m.onPanelScrollbar(y):
		return hover{pointer: pointerHand}
	case m.agentTermShown() && x > listW:
		return hover{}
	case m.refOpen && x >= listW:
		return m.hoverPanel(x, y, listW)
	}
	if s, x0, x1 := m.headerSpan(x, y); clickableSeg(s) {
		return hover{y: y, x0: x0, x1: x1, pointer: pointerHand}
	}
	switch t := m.jiraTab; {
	case t.roadmap != nil, t.plan != nil:
		if y > jiraBodyTop {
			return hover{pointer: pointerHand}
		}
		return hover{}
	case t.inbox != nil:
		if y >= jiraBodyTop && x <= t.inbox.listW+1 && (y-jiraBodyTop+t.inbox.top)/2 < len(t.inbox.rows) {
			return hover{pointer: pointerHand}
		}
		return hover{}
	case t.charts != nil, t.week != nil, t.standup != nil:
		return hover{}
	case t.empty.row >= 0 && y == jiraBodyTop+t.empty.row:
		if s, x0, x1 := segSpan(t.empty.segs, x, 1+t.empty.left); clickableSeg(s) {
			return hover{y: y, x0: x0, x1: x1, pointer: pointerHand}
		}
	}
	switch h := m.hitJira(x, y); {
	case h.band != "":
		return hover{y: y, x0: 1, x1: min(listW, m.width) - 1, pointer: pointerHand}
	case h.line >= 0:
		return hover{pointer: pointerHand} // a card: its cursor shows it already
	}
	return hover{}
}

// clickableSeg is whether a click on header segment s does something.
func clickableSeg(s headSeg) bool {
	return s.kind != "" && (s.kind != "key" || s.press != "")
}

// hoverPanel is hoverAt over the issue panel, left of its scrollbar.
func (m *Model) hoverPanel(x, y, listW int) hover {
	left, right := listW+1, m.width-1
	span := func(x0, x1 int) hover {
		return hover{y: y, x0: max(x0, left), x1: min(x1, right), pointer: pointerHand}
	}
	if m.crumbAt(y) >= 0 {
		return span(left, right)
	}
	i, wrap := m.panelCellAt(y)
	lines := strings.Split(m.refView.GetContent(), "\n")
	if i < 0 || i >= len(lines) {
		return hover{}
	}
	line := lines[i]
	base := left - wrap*m.refView.Width() // the screen column of the line's first cell
	if u, a, b := linkSpan(line, x-base); u != "" {
		return span(base+a, base+b)
	}
	if i == m.activityLine && m.jiraIssue != nil {
		if t, a, b := activityTabSpan(line, x-left, max(m.jiraIssue.CommentTotal, len(m.jiraIssue.Comments))); t >= 0 {
			return span(left+a, left+b)
		}
	}
	h, ok := m.panelHits[i]
	if !ok {
		return hover{}
	}
	at := base + panelIndent(line) // where the hint and action labels start
	switch {
	case len(h.keys) > 0:
		if k, a, b := labelAt(firsts(h.keys), x-at-h.off); k >= 0 {
			return span(at+h.off+a, at+h.off+b)
		}
		return hover{}
	case h.acts:
		if m.jiraIssue == nil || h.field < 0 || h.field >= len(m.jiraIssue.Comments) {
			return hover{}
		}
		if k, a, b := labelAt(firsts(m.commentActions(m.jiraIssue.Comments[h.field])), x-at-h.off); k >= 0 {
			return span(at+h.off+a, at+h.off+b)
		}
		return hover{}
	case h.image != "":
		return hover{pointer: pointerHand}
	case h.double: // a double-click's: a single one does nothing
		return hover{}
	}
	return span(left, right)
}

// underline underlines the cells x0 to x1 of screen row y, less the blanks
// at either end.
func underline(screen string, h hover) string {
	if h.x1 <= h.x0 {
		return screen
	}
	lines := strings.Split(screen, "\n")
	if h.y < 0 || h.y >= len(lines) {
		return screen
	}
	l := lines[h.y]
	plain := ansi.Strip(ansi.Cut(l, h.x0, h.x1))
	x0 := h.x0 + len(plain) - len(strings.TrimLeft(plain, " "))
	x1 := h.x1 - (len(plain) - len(strings.TrimRight(plain, " ")))
	if x1 <= x0 {
		return screen
	}
	const on, off = "\x1b[4m", "\x1b[24m"
	lines[h.y] = ansi.Truncate(l, x0, "") + on + keepBG(ansi.Cut(l, x0, x1), on) + off + ansi.TruncateLeft(l, x1, "")
	return strings.Join(lines, "\n")
}

// pointerOK is whether the terminal gets pointer shapes: not through a
// tmux that swallows them.
func pointerOK() bool {
	return os.Getenv("TMUX") == "" || tmuxPassthrough()
}

// pointerTo sets the mouse pointer's shape, "" for the default; nil when it
// is that already.
func (m *Model) pointerTo(shape string) tea.Cmd {
	if shape == "" {
		shape = "default"
	}
	if !m.pointerOn || shape == m.pointer || (m.pointer == "" && shape == "default") {
		return nil
	}
	m.pointer = shape
	return tea.Raw(m.pointerSeq(shape))
}

func (m *Model) pointerSeq(shape string) string {
	seq := ansi.SetPointerShape(shape)
	if os.Getenv("TMUX") != "" {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// ReleasePointer puts back the default pointer, for after the program
// ends; "" when it never changed.
func (m Model) ReleasePointer() string {
	if m.pointer == "" || m.pointer == "default" {
		return ""
	}
	return m.pointerSeq("default")
}
