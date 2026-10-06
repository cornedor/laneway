package ui

// The board's arrange mode (alt+L): the lane layout it shows, edited in
// place as the browser's settings lane editor edits one (a lanes.Draft over
// the board): each lane's columns listed, the cursor on one. Every change
// is written to ui.lane_layouts at once. Without a layout it starts one
// from the board's columns.

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/lanes"
)

// jiraArrange is the arrange mode's cursor: a board column, an index into
// jiraBoard().
type jiraArrange struct{ col int }

// jiraBoard is the board's columns as its lanes are made of: a kanban
// board's backlog left out of its board view.
func (m *Model) jiraBoard() []jira.Column {
	t := m.jiraTab
	if t.cfg == nil {
		return nil
	}
	skip := -1
	if v, ok := m.jiraCurrentView(); ok && v.kind == jiraViewBoard {
		skip = kanbanBacklog(t.cfg)
	}
	var out []jira.Column
	for i, c := range t.cfg.Columns {
		if i != skip {
			out = append(out, c)
		}
	}
	return out
}

// openJiraArrange starts the arrange mode on the board's layout, a new one
// of its columns when it shows none.
func (m *Model) openJiraArrange() {
	t := m.jiraTab
	if t.cfg == nil || !m.jiraShowsLanes() {
		return
	}
	board := m.jiraBoard()
	if _, ok := m.jiraLayout(); !ok {
		name := "My lanes"
		for n := 2; slices.ContainsFunc(m.uiConfig.LaneLayouts, func(l config.LaneLayout) bool { return l.Name == name }); n++ {
			name = fmt.Sprintf("My lanes %d", n)
		}
		l := config.LaneLayout{Name: name}
		m.writeLaneLayout("", lanes.NewDraft(l, board).Apply(l, board))
		t.layout = name
		if m.store != nil {
			_ = m.store.SetMeta(jiraLayoutKey(m.jiraBoardID()), name)
		}
		if _, ok := m.jiraLayout(); !ok {
			m.status = "this board has too few columns for a lane layout"
			return
		}
	}
	t.arrange = &jiraArrange{col: -1}
	if o := m.arrangeOrder(m.arrangeDraft()); len(o) > 0 {
		t.arrange.col = o[0]
	}
	m.buildJiraLanes()
	m.status = "arrange " + m.jiraLayoutName() + ": h l pick · H L stack · n own lane · x hide · < > move lane · r rename · esc done"
	m.renderJira()
}

func (m *Model) arrangeDraft() lanes.Draft {
	l, _ := m.jiraLayout()
	return lanes.NewDraft(l, m.jiraBoard())
}

// arrangeOrder is the columns as the cursor walks them: lane by lane, then
// the hidden ones.
func (m *Model) arrangeOrder(d lanes.Draft) []int {
	var out []int
	for _, l := range d.Lanes {
		out = append(out, l.Cols...)
	}
	return append(out, d.Hidden...)
}

// arrangeLane is the index of the draft lane holding column ci, -1 for
// none (hidden).
func arrangeLane(d lanes.Draft, ci int) int {
	return slices.IndexFunc(d.Lanes, func(l lanes.DraftLane) bool { return slices.Contains(l.Cols, ci) })
}

// arrangeEdit changes the layout through its draft and writes it.
func (m *Model) arrangeEdit(edit func(d *lanes.Draft)) {
	l, ok := m.jiraLayout()
	if !ok {
		m.jiraTab.arrange = nil
		return
	}
	board := m.jiraBoard()
	d := lanes.NewDraft(l, board)
	edit(&d)
	m.writeLaneLayout(l.Name, d.Apply(l, board))
	m.buildJiraLanes()
	m.renderJira()
}

// take lifts column ci out of its lane or the hidden ones; an emptied lane
// stays for Apply to drop, so lane indexes hold.
func take(d *lanes.Draft, ci int) {
	for i := range d.Lanes {
		d.Lanes[i].Cols = slices.DeleteFunc(d.Lanes[i].Cols, func(c int) bool { return c == ci })
	}
	d.Hidden = slices.DeleteFunc(d.Hidden, func(c int) bool { return c == ci })
}

// writeLaneLayout puts l in ui.lane_layouts in place of the layout named
// old ("" adds it), and writes the config file.
func (m *Model) writeLaneLayout(old string, l config.LaneLayout) {
	ls := slices.Clone(m.uiConfig.LaneLayouts)
	if i := slices.IndexFunc(ls, func(x config.LaneLayout) bool { return x.Name == old }); old != "" && i >= 0 {
		ls[i] = l
	} else {
		ls = append(ls, l)
	}
	next := m.uiConfig
	next.LaneLayouts = ls
	opts, _ := optionsFrom(next)
	m.uiConfig.LaneLayouts, m.opts.laneLayouts = ls, opts.laneLayouts
	if m.configPath == "" {
		m.status = "no config file to write to: the layout lasts till you quit"
		return
	}
	if err := config.SetUI(m.configPath, "lane_layouts", ls); err != nil {
		m.fail("ui.lane_layouts: " + err.Error())
	}
}

func (m Model) handleJiraArrangeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	d := m.arrangeDraft()
	ci := t.arrange.col
	li := arrangeLane(d, ci)
	walk := func(step int) {
		o := m.arrangeOrder(d)
		if len(o) == 0 {
			return
		}
		i := max(slices.Index(o, ci), 0)
		t.arrange.col = o[(i+step+len(o))%len(o)]
		m.renderJira()
	}
	switch s := msg.String(); {
	case s == "esc" || s == "enter" || key.Matches(msg, m.keys.ArrangeLanes):
		t.arrange = nil
		m.status = "lanes: " + m.jiraLayoutName()
		m.buildJiraLanes()
		m.renderJira()
	case s == "h" || s == "left" || s == "k" || s == "up":
		walk(-1)
	case s == "l" || s == "right" || s == "j" || s == "down":
		walk(1)
	case (s == "H" || s == "L") && ci >= 0:
		to := li - 1
		if s == "L" {
			to = li + 1
		}
		if li < 0 { // hidden: onto the last lane
			to = len(d.Lanes) - 1
		}
		if to < 0 || to >= len(d.Lanes) {
			return m, nil
		}
		m.arrangeEdit(func(d *lanes.Draft) {
			take(d, ci)
			d.Lanes[to].Cols = append(d.Lanes[to].Cols, ci)
		})
	case s == "n" && ci >= 0:
		m.arrangeEdit(func(d *lanes.Draft) {
			take(d, ci)
			at := len(d.Lanes)
			if li >= 0 {
				at = li + 1
			}
			d.Lanes = slices.Insert(d.Lanes, at, lanes.DraftLane{Cols: []int{ci}})
		})
	case (s == "x" || s == "delete") && ci >= 0:
		m.arrangeEdit(func(d *lanes.Draft) {
			hidden := li < 0
			take(d, ci)
			if hidden {
				d.Lanes = append(d.Lanes, lanes.DraftLane{Cols: []int{ci}})
			} else {
				d.Hidden = append(d.Hidden, ci)
			}
		})
	case (s == "<" || s == ">") && li >= 0:
		to := li - 1
		if s == ">" {
			to = li + 1
		}
		if to < 0 || to >= len(d.Lanes) {
			return m, nil
		}
		m.arrangeEdit(func(d *lanes.Draft) { d.Lanes[li], d.Lanes[to] = d.Lanes[to], d.Lanes[li] })
	case s == "r" && li >= 0:
		m.openBulkInput("lane-rename", "lane name (empty: its first column's)")
		m.jiraFieldInput.SetValue(d.Lanes[li].Name)
		m.jiraFieldInput.CursorEnd()
		m.jiraFieldKey = m.jiraLayoutName()
	}
	return m, nil
}

// applyLaneRename names the cursor's lane raw.
func (m Model) applyLaneRename(raw string) (tea.Model, tea.Cmd) {
	m.closeJiraField()
	t := m.jiraTab
	if t.arrange == nil {
		return m, nil
	}
	ci := t.arrange.col
	m.arrangeEdit(func(d *lanes.Draft) {
		if li := arrangeLane(*d, ci); li >= 0 {
			d.Lanes[li].Name = strings.TrimSpace(raw)
		}
	})
	return m, nil
}

// renderJiraArrange draws the layout's lanes side by side, each listing its
// columns, the cursor's lit; the hidden columns under them.
func (m *Model) renderJiraArrange(width, height int) string {
	t := m.jiraTab
	board, d := m.jiraBoard(), m.arrangeDraft()
	visible, laneW := jiraLaneLayout(width, len(d.Lanes))
	if li := arrangeLane(d, t.arrange.col); li >= 0 {
		t.firstLane = min(max(t.firstLane, li-visible+1), li)
	}
	t.firstLane = min(max(t.firstLane, 0), max(len(d.Lanes)-visible, 0))
	inner := laneW - 1
	col := func(ci int) string {
		if ci == t.arrange.col {
			return jiraViewActive.Render(ansi.Truncate("▸ "+board[ci].Name, inner, "…"))
		}
		return ansi.Truncate("  "+board[ci].Name, inner, "…")
	}
	var cols [][]string
	for li := t.firstLane; li < t.firstLane+visible && li < len(d.Lanes); li++ {
		l := d.Lanes[li]
		name := l.Name
		if name == "" && len(l.Cols) > 0 {
			name = board[l.Cols[0]].Name
		}
		lines := []string{"▍ " + jiraLaneStyle.Render(ansi.Truncate(name, inner-2, "…")), ""}
		for _, ci := range l.Cols {
			lines = append(lines, col(ci))
		}
		if n := len(l.Foreign); n > 0 {
			lines = append(lines, jiraDimStyle.Render(ansi.Truncate(fmt.Sprintf("  + %d on other boards", n), inner, "…")))
		}
		cols = append(cols, lines)
	}
	sep := shade(jiraDimStyle.Render("│"), 1)
	out := make([]string, max(height, 1))
	for y := range out {
		var b strings.Builder
		for i, c := range cols {
			cell := ""
			if y < len(c) {
				cell = c[y]
			}
			b.WriteString(canvasCell(cell, inner))
			if i < len(cols)-1 {
				b.WriteString(sep)
			}
		}
		out[y] = b.String()
	}
	hidden := []string{jiraDimStyle.Render("hidden:")}
	for _, ci := range d.Hidden {
		hidden = append(hidden, strings.TrimSpace(col(ci)))
	}
	if len(d.Hidden) == 0 {
		hidden = append(hidden, jiraDimStyle.Render("none (x hides a column)"))
	}
	if height >= 3 {
		out[height-2] = ansi.Truncate(" "+strings.Join(hidden, " "), width, "…")
		out[height-1] = jiraDimStyle.Render(ansi.Truncate(" h l pick · H L stack · n own lane · x hide/show · < > move lane · r rename · esc done", width, "…"))
	}
	return strings.Join(out, "\n")
}
