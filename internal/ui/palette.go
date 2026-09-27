package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// The command palette: one filterable list of everything reachable from
// where you are — the focused pane's actions (run as their key would), the
// board's views, quick filters and boards, and the loaded issues.

// paletteSkip are actions not worth a palette row: moving the cursor.
var paletteSkip = map[string]bool{
	"up": true, "down": true, "left": true, "right": true, "top": true, "bottom": true,
	"page_up": true, "page_down": true, "palette": true,
}

// paletteAliases are words an action is found by besides its description.
var paletteAliases = map[string]string{
	"create":      "create add issue",
	"status":      "transition move",
	"move_left":   "transition status",
	"move_right":  "transition status",
	"quick_edit":  "transition status assign",
	"assign":      "owner assignee",
	"comment":     "reply",
	"reply":       "comment",
	"log_work":    "time worklog",
	"timer":       "time worklog",
	"timesheet":   "time worklog",
	"browser":     "web url",
	"copy_url":    "link web",
	"search":      "find filter",
	"goto":        "open find",
	"points":      "estimate",
	"move_sprint": "backlog",
}

// paletteDesc renames actions whose board description is wrong on a
// screen over the board.
var paletteDesc = map[string]map[string]string{
	"roadmap": {"move_left": "move the bar a column earlier", "move_right": "move the bar a column later",
		"end_earlier": "move its end earlier", "end_later": "move its end later", "quit": "close the roadmap"},
	"planning": {"quit": "close planning"},
	"charts":   {"quit": "close the charts"},
}

// openPalette fills the picker with the palette's rows.
func (m *Model) openPalette() {
	m.paletteFocus = m.focus
	m.startJiraPicker(jiraPickPalette, "Command palette", true)
	m.jiraPicker.filter.Placeholder = "action, view, filter, board or issue…"
	scope := m.paletteScope()
	onBoard := scope == "board" || scope == "panel"
	names := m.keys.keyNames()
	t := m.jiraTab
	var items []jiraPickerItem
	var pinned [][2]string
	if onBoard {
		pinned = m.pinnedIssues()
	}
	for _, p := range pinned {
		label := "pinned  " + p[0] + "  " + ansi.Strip(p[1])
		if i := slices.IndexFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == p[0] }); i >= 0 {
			label += "  · " + m.jiraTab.cards[i].Status // on the board: its status now
		}
		items = append(items, jiraPickerItem{id: "i:" + p[0], label: label})
	}
	for _, s := range keyScopes {
		if s.name != scope {
			continue
		}
		for _, name := range s.actions {
			b := names[name]
			if paletteSkip[name] || b == nil || len(b.Keys()) == 0 {
				continue
			}
			desc := b.Help().Desc
			if d, ok := paletteDesc[scope][name]; ok {
				desc = d
			}
			items = append(items, jiraPickerItem{id: "a:" + b.Keys()[0], label: desc + "  " + keysLabel(*b), search: paletteAliases[name]})
		}
	}
	if scope == "board" {
		for i, v := range t.views {
			items = append(items, jiraPickerItem{id: "v:" + strconv.Itoa(i), label: "view  " + v.name, current: i == t.viewIdx})
		}
		for i, q := range t.quick {
			items = append(items, jiraPickerItem{id: "q:" + strconv.Itoa(i), label: "filter  " + q.Name, current: t.quickOn[q.ID]})
		}
		for i, f := range m.opts.filters {
			items = append(items, jiraPickerItem{id: "s:" + strconv.Itoa(i), label: "search  " + f.Name + "  /" + f.Query,
				current: t.search.Value() == f.Query})
		}
		for _, b := range t.boards {
			items = append(items, jiraPickerItem{id: "b:" + strconv.Itoa(b.ID), label: "board  " + b.Name, current: b.ID == m.jiraBoardID()})
		}
	}
	if n := m.hiddenFields(); n > 0 {
		items = append(items, jiraPickerItem{id: "e:", label: fmt.Sprintf("show empty fields  %d hidden", n)})
	}
	if m.lastDownload != "" {
		items = append(items, jiraPickerItem{id: "d:", label: "open download  " + filepath.Base(m.lastDownload)})
	}
	items = append(items, jiraPickerItem{id: "m:", label: fmt.Sprintf("messages  the status line's last %d", len(m.statusLog))})
	if onBoard {
		for _, c := range t.cards {
			if id := "i:" + c.Key; !slices.ContainsFunc(items, func(it jiraPickerItem) bool { return it.id == id }) {
				items = append(items, jiraPickerItem{id: id, label: c.Key + "  " + ansi.Strip(c.Summary)})
			}
		}
		for _, r := range m.recentIssues() {
			if id := "i:" + r[0]; !slices.ContainsFunc(items, func(it jiraPickerItem) bool { return it.id == id }) {
				items = append(items, jiraPickerItem{id: id, label: "recent  " + r[0] + "  " + ansi.Strip(r[1])})
			}
		}
	}
	m.setJiraPickerItems(items)
	m.jiraPicker.idx = 0
}

// paletteScope is the keyScopes entry the palette offers. A screen over the
// board offers its own keys only: board rows would run as them.
func (m *Model) paletteScope() string {
	t := m.jiraTab
	switch {
	case m.paletteFocus == focusRef && m.refOpen:
		return "panel"
	case t.roadmap != nil:
		return "roadmap"
	case t.plan != nil:
		return "planning"
	case t.charts != nil:
		return "charts"
	}
	return "board"
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
	case "s":
		i, _ := strconv.Atoi(arg)
		if i < len(m.opts.filters) {
			m.jiraTab.search.SetValue(m.opts.filters[i].Query)
			m.applyJiraSearch()
			m.status = "/" + m.opts.filters[i].Query + " · esc clears"
		}
		return m, nil
	case "b":
		return m, m.pickJiraBoard(jiraPickBoard, arg)
	case "m":
		m.openMessages()
		return m, nil
	case "e":
		m.showEmpty = true
		m.renderRef()
		return m, nil
	case "d":
		m.status = "opening " + m.lastDownload + "…"
		return m, m.openOpenable(openable{name: filepath.Base(m.lastDownload), url: m.lastDownload})
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

// The palette also searches all of Jira once three characters are typed:
// the hits come after its own rows, marked ⌕.
const (
	paletteSearchMin   = 3
	paletteSearchDelay = 300 * time.Millisecond
	paletteSearchHits  = 20
)

type paletteSearchMsg struct{ seq int }

type paletteFoundMsg struct {
	seq   int
	cards []jira.Card
	err   error
}

// schedulePaletteSearch arms a search for the filter once typing pauses.
func (m *Model) schedulePaletteSearch() tea.Cmd {
	m.jiraPicker.fetchSeq++
	if scope := m.paletteScope(); scope != "board" && scope != "panel" ||
		len([]rune(strings.TrimSpace(m.jiraPicker.filter.Value()))) < paletteSearchMin {
		return nil
	}
	seq := m.jiraPicker.fetchSeq
	return tea.Tick(paletteSearchDelay, func(time.Time) tea.Msg { return paletteSearchMsg{seq} })
}

func (m Model) handlePaletteSearch(msg paletteSearchMsg) (tea.Model, tea.Cmd) {
	p := m.jiraPicker
	if !p.active || p.kind != jiraPickPalette || msg.seq != p.fetchSeq {
		return m, nil
	}
	c, ctx, q := m.jiraClient, m.ctx, p.filter.Value()
	return m, func() tea.Msg {
		cards, err := c.FindIssues(ctx, q, paletteSearchHits)
		return paletteFoundMsg{msg.seq, cards, err}
	}
}

// handlePaletteFound adds the hits not already listed.
func (m Model) handlePaletteFound(msg paletteFoundMsg) (tea.Model, tea.Cmd) {
	p := &m.jiraPicker
	if !p.active || p.kind != jiraPickPalette || msg.seq != p.fetchSeq {
		return m, nil
	}
	p.found = nil
	if msg.err != nil {
		m.fail("issue search: " + msg.err.Error()) // not "no matches"
	}
	for _, c := range msg.cards {
		id := "i:" + c.Key
		if slices.ContainsFunc(p.all, func(it jiraPickerItem) bool { return it.id == id }) {
			continue
		}
		p.found = append(p.found, jiraPickerItem{id: id, label: "⌕ " + c.Key + "  " + ansi.Strip(c.Summary)})
	}
	idx := p.idx
	m.filterJiraPicker()
	p.idx = min(idx, max(len(p.items)-1, 0))
	return m, nil
}

// Issues opened in the panel lately, newest first, for the palette.
const (
	recentMeta = jiraMetaPrefix + "recent"
	recentMax  = 20
)

// recentIssues are the recently opened issues as [key, summary].
func (m *Model) recentIssues() [][2]string {
	if m.store == nil {
		return nil
	}
	v, ok, _ := m.store.GetMeta(recentMeta)
	var out [][2]string
	if ok {
		_ = json.Unmarshal([]byte(v), &out)
	}
	return out
}

// rememberRecent puts key first among the recent issues.
func (m *Model) rememberRecent(key, summary string) {
	if m.store == nil || key == "" {
		return
	}
	r := slices.DeleteFunc(m.recentIssues(), func(e [2]string) bool { return e[0] == key })
	r = append([][2]string{{key, summary}}, r...)
	b, _ := json.Marshal(r[:min(len(r), recentMax)])
	_ = m.store.SetMeta(recentMeta, string(b))
}

// pinnedMeta holds the issues pinned with * in the panel, oldest first; the
// palette lists them before anything else.
const pinnedMeta = jiraMetaPrefix + "pinned"

// pinnedIssues are the pinned issues as [key, summary].
func (m *Model) pinnedIssues() [][2]string {
	if m.store == nil {
		return nil
	}
	v, ok, _ := m.store.GetMeta(pinnedMeta)
	var out [][2]string
	if ok {
		_ = json.Unmarshal([]byte(v), &out)
	}
	return out
}

// togglePin pins an issue, or unpins it.
func (m *Model) togglePin(key, summary string) {
	if m.store == nil || key == "" {
		return
	}
	p := m.pinnedIssues()
	if kept := slices.DeleteFunc(slices.Clone(p), func(e [2]string) bool { return e[0] == key }); len(kept) < len(p) {
		p = kept
		m.status = "unpinned " + key
	} else {
		p = append(p, [2]string{key, summary})
		m.status = "pinned " + key + " · first in the palette"
	}
	b, _ := json.Marshal(p)
	_ = m.store.SetMeta(pinnedMeta, string(b))
	m.loadPins()
	m.jiraTab.rows = nil
	m.renderJira()
}

// loadPins caches the pinned keys for the cards' ★.
func (m *Model) loadPins() {
	m.pins = map[string]bool{}
	for _, p := range m.pinnedIssues() {
		m.pins[p[0]] = true
	}
}
