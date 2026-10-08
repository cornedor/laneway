package ui

// The board's arrange mode (alt+L): the lane layout it shows, edited in
// place as the browser's settings lane editor edits one (a lanes.Draft over
// the board): each lane's columns listed, the cursor on one, a column split
// over lanes listed status by status. Every change is written to
// ui.lane_layouts at once. Without a layout it starts one from the board's
// columns.

import (
	"cmp"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/lanes"
)

// jiraArrange is the arrange mode's cursor, a piece of the draft, and the
// columns s split though their statuses still sit together.
type jiraArrange struct {
	at    lanes.Piece
	split []int
}

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
		name := i18n.T("My lanes")
		for n := 2; slices.ContainsFunc(m.uiConfig.LaneLayouts, func(l config.LaneLayout) bool { return l.Name == name }); n++ {
			name = i18n.Tf("My lanes %d", n)
		}
		l := config.LaneLayout{Name: name}
		m.writeLaneLayout("", lanes.NewDraft(l, board).Apply(l, board))
		t.layout = name
		if m.store != nil {
			_ = m.store.SetMeta(jiraLayoutKey(m.jiraBoardID()), name)
		}
		if _, ok := m.jiraLayout(); !ok {
			m.status = i18n.T("this board has too few columns for a lane layout")
			return
		}
	}
	t.arrange = &jiraArrange{at: lanes.Piece{Col: -1}}
	if o := m.arrangeOrder(m.arrangeDraft()); len(o) > 0 {
		t.arrange.at = o[0]
	}
	m.buildJiraLanes()
	m.status = i18n.Tf("arrange %s: h l pick · H L stack · n own lane · x hide · s split · < > move lane · r rename · esc done", m.jiraLayoutName())
	m.renderJira()
}

// arrangeDraft is the layout's draft over the board, the columns s split
// status by status.
func (m *Model) arrangeDraft() lanes.Draft {
	l, _ := m.jiraLayout()
	board := m.jiraBoard()
	d := lanes.NewDraft(l, board)
	if a := m.jiraTab.arrange; a != nil {
		for _, ci := range a.split {
			d.Split(ci, board)
		}
	}
	return d
}

// arrangeOrder is the pieces as the cursor walks them: lane by lane, then
// the hidden ones.
func (m *Model) arrangeOrder(d lanes.Draft) []lanes.Piece {
	var out []lanes.Piece
	for _, l := range d.Lanes {
		out = append(out, l.Pieces...)
	}
	return append(out, d.Hidden...)
}

// arrangeEdit changes the layout through its draft and writes it; the
// cursor then stays on its piece, or its column's, whole or split.
func (m *Model) arrangeEdit(edit func(d *lanes.Draft)) {
	l, ok := m.jiraLayout()
	if !ok {
		m.jiraTab.arrange = nil
		return
	}
	board := m.jiraBoard()
	d := m.arrangeDraft()
	edit(&d)
	m.writeLaneLayout(l.Name, d.Apply(l, board))
	a := m.jiraTab.arrange
	if o := m.arrangeOrder(m.arrangeDraft()); !slices.Contains(o, a.at) {
		if i := slices.IndexFunc(o, func(p lanes.Piece) bool { return p.Col == a.at.Col }); i >= 0 {
			a.at = o[i]
		}
	}
	m.buildJiraLanes()
	m.renderJira()
}

// writeLaneLayout puts l in ui.lane_layouts in place of the layout named
// old ("" adds it), and writes the config file. A layout without a site
// gets this one's.
func (m *Model) writeLaneLayout(old string, l config.LaneLayout) {
	if l.Site == "" {
		l.Site = m.jiraSite()
	}
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
		m.status = i18n.T("no config file to write to: the layout lasts till you quit")
		return
	}
	if err := config.SetUI(m.configPath, "lane_layouts", ls); err != nil {
		m.fail(i18n.Tf("ui.lane_layouts: %s", err.Error()))
	}
}

func (m Model) handleJiraArrangeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	d := m.arrangeDraft()
	p := t.arrange.at
	li := d.LaneOf(p)
	board := m.jiraBoard()
	walk := func(step int) {
		o := m.arrangeOrder(d)
		if len(o) == 0 {
			return
		}
		i := max(slices.Index(o, p), 0)
		t.arrange.at = o[(i+step+len(o))%len(o)]
		m.renderJira()
	}
	switch s := msg.String(); {
	case s == "esc" || s == "enter" || key.Matches(msg, m.keys.ArrangeLanes):
		t.arrange = nil
		m.status = i18n.Tf("lanes: %s", m.jiraLayoutName())
		m.buildJiraLanes()
		m.renderJira()
	case s == "h" || s == "left" || s == "k" || s == "up":
		walk(-1)
	case s == "l" || s == "right" || s == "j" || s == "down":
		walk(1)
	case (s == "H" || s == "L") && p.Col >= 0:
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
			d.Take(p)
			d.Lanes[to].Pieces = append(d.Lanes[to].Pieces, p)
		})
	case s == "n" && p.Col >= 0:
		m.arrangeEdit(func(d *lanes.Draft) {
			d.Take(p)
			at := len(d.Lanes)
			if li >= 0 {
				at = li + 1
			}
			d.Lanes = slices.Insert(d.Lanes, at, lanes.DraftLane{Pieces: []lanes.Piece{p}})
		})
	case (s == "x" || s == "delete") && p.Col >= 0:
		m.arrangeEdit(func(d *lanes.Draft) {
			hidden := li < 0
			d.Take(p)
			if hidden {
				d.Lanes = append(d.Lanes, lanes.DraftLane{Pieces: []lanes.Piece{p}})
			} else {
				d.Hidden = append(d.Hidden, p)
			}
		})
	case s == "s" && p.Col >= 0 && p.Status == "":
		if len(board[p.Col].StatusIDs) < 2 {
			m.status = i18n.Tf("%s has one status", board[p.Col].Name)
			return m, nil
		}
		t.arrange.split = append(t.arrange.split, p.Col)
		t.arrange.at = lanes.Piece{Col: p.Col, Status: board[p.Col].StatusIDs[0]}
		m.status = i18n.Tf("%s split: H L move a status to another lane · s gathers them here", board[p.Col].Name)
		m.renderJira()
	case s == "s" && p.Status != "":
		whole := lanes.Piece{Col: p.Col}
		t.arrange.split = slices.DeleteFunc(t.arrange.split, func(c int) bool { return c == p.Col })
		t.arrange.at = whole
		m.arrangeEdit(func(d *lanes.Draft) {
			d.Take(whole)
			if li >= 0 {
				d.Lanes[li].Pieces = append(d.Lanes[li].Pieces, whole)
			} else {
				d.Hidden = append(d.Hidden, whole)
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
		m.openBulkInput("lane-rename", i18n.T("lane name (empty: its first column's)"))
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
	p := t.arrange.at
	m.arrangeEdit(func(d *lanes.Draft) {
		if li := d.LaneOf(p); li >= 0 {
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
	if li := d.LaneOf(t.arrange.at); li >= 0 {
		t.firstLane = min(max(t.firstLane, li-visible+1), li)
	}
	t.firstLane = min(max(t.firstLane, 0), max(len(d.Lanes)-visible, 0))
	inner := laneW - 1
	piece := func(p lanes.Piece) string {
		name := board[p.Col].Name
		if p.Status != "" {
			name += " › " + cmp.Or(t.statusNames[p.Status], p.Status)
		}
		if p == t.arrange.at {
			return jiraViewActive.Render(ansi.Truncate("▸ "+name, inner, "…"))
		}
		return ansi.Truncate("  "+name, inner, "…")
	}
	var cols [][]string
	for li := t.firstLane; li < t.firstLane+visible && li < len(d.Lanes); li++ {
		l := d.Lanes[li]
		name := l.Name
		if name == "" && len(l.Pieces) > 0 {
			ps := slices.SortedFunc(slices.Values(l.Pieces), func(a, b lanes.Piece) int { return a.Col - b.Col })
			p := ps[0]
			if i := slices.IndexFunc(ps, func(p lanes.Piece) bool { return p.Status == "" }); i >= 0 {
				p = ps[i]
			}
			name = cmp.Or(t.statusNames[p.Status], board[p.Col].Name)
		}
		lines := []string{"▍ " + jiraLaneStyle.Render(ansi.Truncate(name, inner-2, "…")), ""}
		for _, p := range l.Pieces {
			lines = append(lines, piece(p))
		}
		if n := len(l.Foreign); n > 0 {
			lines = append(lines, jiraDimStyle.Render(ansi.Truncate(i18n.Tf("  + %d on other boards", n), inner, "…")))
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
	hidden := []string{jiraDimStyle.Render(i18n.T("hidden:"))}
	for _, p := range d.Hidden {
		hidden = append(hidden, strings.TrimSpace(piece(p)))
	}
	if len(d.Hidden) == 0 {
		hidden = append(hidden, jiraDimStyle.Render(i18n.T("none (x hides a column)")))
	}
	if height >= 3 {
		out[height-2] = ansi.Truncate(" "+strings.Join(hidden, " "), width, "…")
		out[height-1] = jiraDimStyle.Render(ansi.Truncate(i18n.T(" h l pick · H L stack · n own lane · x hide/show · s split/gather · < > move lane · r rename · esc done"), width, "…"))
	}
	return strings.Join(out, "\n")
}
