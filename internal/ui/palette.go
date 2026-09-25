package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The command palette: one filterable list of everything reachable from
// where you are — the focused pane's actions (run as their key would), the
// board's views, quick filters and boards, and the loaded issues.

// paletteSkip are actions not worth a palette row: moving the cursor.
var paletteSkip = map[string]bool{
	"up": true, "down": true, "left": true, "right": true, "top": true, "bottom": true,
	"page_up": true, "page_down": true, "palette": true,
}

// openPalette fills the picker with the palette's rows.
func (m *Model) openPalette() {
	m.paletteFocus = m.focus
	m.startJiraPicker(jiraPickPalette, "Command palette", true)
	m.jiraPicker.filter.Placeholder = "action, view, filter, board or issue…"
	scope := "board"
	if m.focus == focusRef && m.refOpen {
		scope = "panel"
	}
	names := m.keys.keyNames()
	var items []jiraPickerItem
	for _, s := range keyScopes {
		if s.name != scope {
			continue
		}
		for _, name := range s.actions {
			b := names[name]
			if paletteSkip[name] || b == nil || len(b.Keys()) == 0 {
				continue
			}
			items = append(items, jiraPickerItem{id: "a:" + b.Keys()[0], label: b.Help().Desc + "  " + keysLabel(*b)})
		}
	}
	t := m.jiraTab
	if scope == "board" {
		for i, v := range t.views {
			items = append(items, jiraPickerItem{id: "v:" + strconv.Itoa(i), label: "view  " + v.name, current: i == t.viewIdx})
		}
		for i, q := range t.quick {
			items = append(items, jiraPickerItem{id: "q:" + strconv.Itoa(i), label: "filter  " + q.Name, current: t.quickOn[q.ID]})
		}
		for _, b := range t.boards {
			items = append(items, jiraPickerItem{id: "b:" + strconv.Itoa(b.ID), label: "board  " + b.Name, current: b.ID == m.jiraBoardID()})
		}
	}
	for _, c := range t.cards {
		items = append(items, jiraPickerItem{id: "i:" + c.Key, label: c.Key + "  " + ansi.Strip(c.Summary)})
	}
	m.setJiraPickerItems(items)
	m.jiraPicker.idx = 0
}

// applyPalette runs the picked row.
func (m Model) applyPalette(id string) (tea.Model, tea.Cmd) {
	kind, arg, _ := strings.Cut(id, ":")
	switch kind {
	case "a":
		m.focus = m.paletteFocus
		return m.handleKey(keyPress(arg))
	case "v":
		i, _ := strconv.Atoi(arg)
		return m, m.cycleJiraView(i - m.jiraTab.viewIdx)
	case "q":
		i, _ := strconv.Atoi(arg)
		return m, m.toggleJiraQuick(i)
	case "b":
		return m, m.pickJiraBoard(jiraPickBoard, arg)
	case "i":
		m.selectJiraKey(arg)
		m.renderJira()
		return m.openJiraKey(arg)
	}
	return m, nil
}

// keyPress is the key event a binding's key string names.
func keyPress(s string) tea.KeyPressMsg {
	named := map[string]rune{
		"enter": tea.KeyEnter, "tab": tea.KeyTab, "esc": tea.KeyEscape, "backspace": tea.KeyBackspace,
		"space": tea.KeySpace, "left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "delete": tea.KeyDelete,
	}
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod |= tea.ModCtrl
			s = s[len("ctrl+"):]
			continue
		case strings.HasPrefix(s, "shift+"):
			mod |= tea.ModShift
			s = s[len("shift+"):]
			continue
		case strings.HasPrefix(s, "alt+"):
			mod |= tea.ModAlt
			s = s[len("alt+"):]
			continue
		}
		break
	}
	if c, ok := named[s]; ok {
		k := tea.Key{Code: c, Mod: mod}
		if c == tea.KeySpace && mod == 0 {
			k.Text = " "
		}
		return tea.KeyPressMsg(k)
	}
	r := []rune(s)
	if len(r) != 1 {
		return tea.KeyPressMsg{}
	}
	if mod != 0 {
		return tea.KeyPressMsg{Code: r[0], Mod: mod}
	}
	return tea.KeyPressMsg{Code: r[0], Text: s}
}
