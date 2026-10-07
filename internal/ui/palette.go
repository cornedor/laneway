package ui

import (
	"charm.land/bubbles/v2/key"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/index"
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
	"releases":    "versions fix version",
	"agents":      "herdr worktrees work",
}

// paletteDesc renames actions whose board description is wrong on a
// screen over the board.
var paletteDesc = map[string]map[string]string{
	"roadmap": {"move_left": "move the bar a column earlier", "move_right": "move the bar a column later",
		"end_earlier": "move its end earlier", "end_later": "move its end later", "quit": "close the roadmap"},
	"planning": {"quit": "close planning"},
	"panel":    {"search": "find in the issue"},
	"charts":   {"quit": "close the charts"},
	"standup":  {"quit": "close the standup", "open": "open the issue", "prev_view": "a workday further back", "next_view": "a workday later"},
	"inbox":    {"quit": "close the inbox", "open": "open the thread's issue"},
	"agents":   {"quit": "close the agents", "open": "attach to the agent", "toggle_panel": "open the issue"},
	"week":     {"quit": "close the week", "open": "log work in the cell", "prev_view": "previous week", "next_view": "next week"},
}

// helpDescs is what the ? help says each key does on scope's screen, by
// its keys as help shows them: the palette labels an action the same way.
func (m *Model) helpDescs(scope string) map[string]string {
	out := map[string]string{}
	for _, sec := range m.helpSections() {
		if !strings.EqualFold(sec.title, scope) {
			continue
		}
		for _, r := range sec.rows {
			if _, ok := out[r.keys]; !ok {
				out[r.keys] = r.desc
			}
		}
	}
	return out
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
	if onBoard && m.branchKey != "" {
		items = append(items, jiraPickerItem{id: "i:" + m.branchKey, label: "branch  " + m.branchKey})
	}
	for _, p := range pinned {
		if p[0] == m.branchKey {
			continue // the branch row has it
		}
		label := "pinned  " + p[0] + "  " + ansi.Strip(p[1])
		if i := slices.IndexFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == p[0] }); i >= 0 {
			label += "  · " + m.jiraTab.cards[i].Status // on the board: its status now
		}
		items = append(items, jiraPickerItem{id: "i:" + p[0], label: label})
	}
	help := m.helpDescs(scope)
	for _, s := range keyScopes {
		if s.name != scope {
			continue
		}
		for _, name := range s.actions {
			b := names[name]
			if paletteSkip[name] || b == nil {
				continue
			}
			desc, search := b.Help().Desc, paletteAliases[name]
			if d, ok := paletteDesc[scope][name]; ok {
				desc = d
			} else if d, ok := help[keysLabel(*b)]; ok {
				desc, search = d, strings.TrimSpace(search+" "+desc)
			}
			if len(b.Keys()) == 0 { // unbound (ui.keys none): run by name
				items = append(items, jiraPickerItem{id: "n:" + name, label: desc, search: search})
				continue
			}
			items = append(items, jiraPickerItem{id: "a:" + b.Keys()[0], label: desc + "  " + keysLabel(*b), search: search})
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
	if onBoard {
		for _, k := range slices.Sorted(maps.Keys(m.agents)) {
			for _, a := range m.agents[k] {
				items = append(items, jiraPickerItem{id: "g:" + k + ":" + a.PaneID,
					label: "agent  " + k + "  " + string(a.Status) + "  " + a.Name, search: "agents herdr attach"})
			}
		}
	}
	for i, a := range m.actions {
		if actionOn(a, scope == "panel") && onBoard {
			label := "action  " + a.Name
			if a.Key != "" {
				label += "  " + a.Key
			}
			items = append(items, jiraPickerItem{id: "x:" + strconv.Itoa(i), label: label})
		}
	}
	if n := m.hiddenFields(); n > 0 {
		items = append(items, jiraPickerItem{id: "e:", label: fmt.Sprintf("show empty fields  %d hidden", n)})
	}
	if m.lastDownload != "" {
		items = append(items, jiraPickerItem{id: "d:", label: "open download  " + filepath.Base(m.lastDownload)})
	}
	if m.newRelease != "" {
		items = append(items, jiraPickerItem{id: "u:", label: "update  " + m.newRelease + " is out  " + m.upgradeHint(), search: "upgrade release version"})
	}
	items = append(items, jiraPickerItem{id: "m:", label: fmt.Sprintf("messages  the status line's last %d", len(m.statusLog))})
	if m.queued > 0 {
		items = append(items, jiraPickerItem{id: "w:", label: fmt.Sprintf("queue  %s waiting to reach Jira", plural(m.queued, "write")), search: "offline"})
	}
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
	case t.week != nil:
		return "week"
	case t.standup != nil:
		return "standup"
	case t.inbox != nil:
		return "inbox"
	case t.agentsView != nil:
		return "agents"
	case t.mrs != nil:
		return "merge_requests"
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
	case "n":
		return m.runUnbound(arg)
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
	case "w":
		m.openQueue()
		return m, nil
	case "x":
		i, _ := strconv.Atoi(arg)
		m.focus = m.paletteFocus
		return m, m.runAction(i, m.focus == focusRef && m.refOpen)
	case "e":
		m.showEmpty = true
		m.renderRef()
		return m, nil
	case "d":
		m.status = "opening " + m.lastDownload + "…"
		return m, m.openOpenable(openable{name: filepath.Base(m.lastDownload), url: m.lastDownload})
	case "u":
		return m, m.upgrade()
	case "g":
		k, pane, _ := strings.Cut(arg, ":")
		return m, m.attachAgent(k, pane)
	case "i":
		m.selectJiraKey(arg)
		m.renderJira()
		return m.openJiraKey(arg)
	}
	return m, nil
}

// keyPress is the key event a binding's key string names.
// unboundKey is a key no terminal sends: an unbound action holds it while
// the palette runs it.
const unboundKey = "\U0010FFFD"

// runUnbound runs an action ui.keys left without a key, as its key would.
func (m Model) runUnbound(name string) (tea.Model, tea.Cmd) {
	b := m.keys.keyNames()[name]
	if b == nil {
		return m, nil
	}
	saved := *b
	*b = key.NewBinding(key.WithKeys(unboundKey))
	m.focus = m.paletteFocus
	out, cmd := m.handleKey(keyPress(unboundKey))
	if mm, ok := out.(Model); ok {
		*mm.keys.keyNames()[name] = saved
		out = mm
	}
	return out, cmd
}

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
// the hits come after its own rows, marked ⌕. The index answers first, its
// hits marked with when they were read; Jira's replace them as they come.
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
	// indexed are the index's hits, sent before Jira's (cards is then nil).
	indexed []index.Hit
	local   bool
}

// schedulePaletteSearch arms a search for the filter once typing pauses.
func (m *Model) schedulePaletteSearch() tea.Cmd {
	m.jiraPicker.fetchSeq++
	m.jiraPicker.remote, m.jiraPicker.indexed = nil, nil
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
	c, ctx, ix, q := m.jiraClient, m.ctx, m.index, p.filter.Value()
	remote := func() tea.Msg {
		cards, err := c.FindIssues(ctx, q, paletteSearchHits)
		return paletteFoundMsg{seq: msg.seq, cards: cards, err: err}
	}
	if ix == nil {
		return m, remote
	}
	local := func() tea.Msg {
		hits, _ := ix.Search(q, paletteSearchHits)
		return paletteFoundMsg{seq: msg.seq, indexed: hits, local: true}
	}
	return m, tea.Batch(local, remote)
}

// handlePaletteFound adds the hits not already listed: Jira's, then the
// index's Jira didn't find (a key, a word's middle), whichever lands first.
func (m Model) handlePaletteFound(msg paletteFoundMsg) (tea.Model, tea.Cmd) {
	p := &m.jiraPicker
	if !p.active || p.kind != jiraPickPalette || msg.seq != p.fetchSeq {
		return m, nil
	}
	if msg.local {
		now := time.Now()
		p.indexed = nil
		for _, h := range msg.indexed {
			p.indexed = append(p.indexed, jiraPickerItem{id: "i:" + h.Card.Key, label: "⌕ " + h.Card.Key + "  " + ansi.Strip(h.Card.Summary) + "  · " + draftWhen(h.Synced, now)})
		}
	} else {
		p.remote = nil
		switch {
		case msg.err != nil && jira.Offline(msg.err) && m.index != nil:
			m.status = "offline: issues from the index"
		case msg.err != nil:
			m.fail("issue search: " + msg.err.Error()) // not "no matches"
		}
		for _, c := range msg.cards {
			p.remote = append(p.remote, jiraPickerItem{id: "i:" + c.Key, label: "⌕ " + c.Key + "  " + ansi.Strip(c.Summary)})
		}
	}
	p.found = nil
	for _, it := range append(slices.Clone(p.remote), p.indexed...) {
		listed := func(l []jiraPickerItem) bool {
			return slices.ContainsFunc(l, func(o jiraPickerItem) bool { return o.id == it.id })
		}
		if !listed(p.all) && !listed(p.found) {
			p.found = append(p.found, it)
		}
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
