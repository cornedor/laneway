package ui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/textwidth"
)

// A dropdown is the picker drawn at a point over the board, just the size
// its rows need, the board showing round it: the menu a right-click on a
// card opens, and beside it the submenu one of its rows opens (status,
// priority, assignee, sprint). esc or ← goes back from a submenu to the
// menu; a click outside both closes them.

type point struct{ x, y int }

// dropdownState is the card menu open: its card, where it opened and the
// row picked; parent is the menu's box, drawn under a submenu, at top, left.
type dropdownState struct {
	key       string
	at        point
	row       int
	parent    string
	top, left int
}

// dropdownWidth is a filterable dropdown's width: one whatever the results,
// so the box keeps still while they come.
const dropdownWidth = 36

// openCardMenu opens key's menu with its top left corner at at, row idx
// selected.
func (m *Model) openCardMenu(key string, at point, idx int) {
	m.openQuickEditKey(key)
	m.setJiraPickerItems(m.cardMenu())
	m.jiraPicker.idx = min(max(idx, 0), len(m.jiraPicker.items)-1)
	m.jiraPicker.at = &at
	m.dropdown = &dropdownState{key: key, at: at}
}

// dropdownOn is whether the picker is drawn as a dropdown, over nothing
// else.
func (m *Model) dropdownOn() bool {
	return m.jiraPicker.at != nil && m.pickerOnTop() && !m.pickerInline()
}

// dropdownWin is how many rows a dropdown lists in maxH.
func (m *Model) dropdownWin(maxH int) int {
	win := maxH - 4 // the borders and the scroll markers
	if m.jiraPicker.filterable {
		win = min(win-1, 10)
	}
	return max(win, 3)
}

// renderDropdown draws the picker as a dropdown: a border round its rows,
// each row a mark, its label and, dim at the right, its hint.
func (m *Model) renderDropdown(maxH int) string {
	p := &m.jiraPicker
	var rows []string
	sel := -1
	if p.filterable {
		p.filter.SetWidth(dropdownWidth - 2 - ansi.StringWidth(p.filter.Prompt) - 2)
		rows = append(rows, p.filter.View())
	}
	switch {
	case p.loading:
		rows = append(rows, refDimStyle.Render(" loading…"))
	case p.err != nil:
		rows = append(rows, refErrStyle.Render(" "+p.err.Error()))
	case len(p.items) == 0:
		rows = append(rows, refDimStyle.Render(" "+p.emptyText()))
	default:
		start, end := m.pickerWindow(m.dropdownWin(maxH))
		marks := slices.ContainsFunc(p.items, func(it jiraPickerItem) bool { return it.current })
		if start > 0 {
			rows = append(rows, refDimStyle.Render("  ↑ more"))
		}
		for i := start; i < end; i++ {
			it := p.items[i]
			row := " " + it.label
			if marks { // a ✓ column only where a row has one
				mark := "  "
				if it.current {
					mark = "✓ "
				}
				row = " " + mark + it.label
			}
			if i == p.idx {
				sel = len(rows)
			}
			if it.hint != "" {
				row += "\t" + it.hint // right-aligned below
			}
			rows = append(rows, row)
		}
		if end < len(p.items) {
			rows = append(rows, refDimStyle.Render("  ↓ more"))
		}
	}
	w := 0
	for _, r := range rows {
		w = max(w, textwidth.Width(strings.Replace(r, "\t", "   ", 1))+1)
	}
	if p.filterable {
		w = dropdownWidth - 2
	}
	w = min(w, m.width-2)
	for i, r := range rows {
		label, hint, _ := strings.Cut(r, "\t")
		hint = refDimStyle.Render(hint)
		pad := w - textwidth.Width(label) - textwidth.Width(hint)
		if pad < 1 {
			label, pad = ansi.Truncate(label, max(w-textwidth.Width(hint)-1, 1), "…"), 1
			pad = max(w-textwidth.Width(label)-textwidth.Width(hint), 0)
		}
		r = label + strings.Repeat(" ", pad) + hint
		if i == sel {
			r = selectedRow.Render(ansi.Strip(r))
		}
		rows[i] = r
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).
		Render(strings.Join(rows, "\n"))
}

// dropdownPlace is where box goes: its corner at the picker's point, moved
// in so it fits the body.
func (m *Model) dropdownPlace(box string) (top, left int) {
	at := m.jiraPicker.at
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	return max(min(at.y, m.bodyH()-h), 0), max(min(at.x, m.width-w), 0)
}

// pickerPlace is where the picker's box is drawn, and whether x, y is on
// it: centred, or a dropdown's place.
func (m *Model) pickerPlace(box string, x, y int) (top, left int, inside bool) {
	if m.jiraPicker.at == nil {
		return m.overlayAt(box, x, y)
	}
	top, left = m.dropdownPlace(box)
	return top, left, y >= top && y < top+lipgloss.Height(box) && x >= left && x < left+lipgloss.Width(box)
}

// dropdownRowAt is the dropdown's item on screen row y, -1 for none;
// outside is whether x, y is off its box.
func (m *Model) dropdownRowAt(x, y int) (idx int, outside bool) {
	bodyH := m.bodyH()
	top, _, inside := m.pickerPlace(m.renderDropdown(bodyH), x, y)
	if !inside {
		return -1, true
	}
	p := &m.jiraPicker
	if p.loading || p.err != nil || len(p.items) == 0 {
		return -1, false
	}
	start, end := m.pickerWindow(m.dropdownWin(bodyH))
	first := top + 1 // the border
	if p.filterable {
		first++
	}
	if start > 0 {
		first++
	}
	if i := start + y - first; y >= first && i < end {
		return i, false
	}
	return -1, false
}

// drawDropdown draws the dropdown, and the menu under a submenu, over body.
func (m *Model) drawDropdown(body string, bodyH int) string {
	if d := m.dropdown; d != nil && d.parent != "" {
		body = placeBox(body, d.parent, d.top, d.left)
	}
	box := m.renderDropdown(bodyH)
	top, left := m.dropdownPlace(box)
	return placeBox(body, box, top, left)
}

// placeBox draws box over screen with its corner at top, left.
func placeBox(screen, box string, top, left int) string {
	lines := strings.Split(screen, "\n")
	for i, b := range strings.Split(box, "\n") {
		y := top + i
		if y < 0 || y >= len(lines) {
			continue
		}
		l := lines[y]
		lines[y] = ansi.Truncate(l, left, "") + "\x1b[m" + strings.Repeat(" ", max(left-ansi.StringWidth(l), 0)) + b + "\x1b[m" +
			ansi.TruncateLeft(l, left+lipgloss.Width(b), "")
	}
	return strings.Join(lines, "\n")
}

// openSubmenu anchors the picker a card menu's row just opened beside that
// row, the menu kept drawn under it.
func (m *Model) openSubmenu(d *dropdownState, box string, top, left, row int) {
	if !m.jiraPicker.active {
		return
	}
	d.parent, d.top, d.left, d.row = box, top, left, row
	m.jiraPicker.at = &point{x: left + lipgloss.Width(box) - 1, y: top + row}
	if w := dropdownWidth; m.jiraPicker.at.x+w > m.width { // no room right: open to the left
		m.jiraPicker.at.x = max(left-w+1, 0)
	}
	m.dropdown = d
}

// backToMenu leaves a submenu for the card menu it opened from; false when
// the picker is no submenu.
func (m *Model) backToMenu() bool {
	d := m.dropdown
	if d == nil || d.parent == "" || m.jiraPicker.at == nil {
		return false
	}
	m.closeJiraPicker()
	m.openCardMenu(d.key, d.at, d.row)
	return true
}
