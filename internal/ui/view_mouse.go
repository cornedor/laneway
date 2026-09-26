package ui

import (
	"time"

	"github.com/cornedor/laneway/internal/jira"

	tea "charm.land/bubbletea/v2"
)

// Clicks and the wheel in the views that take the board's place: the
// roadmap's rows and planning's two sides. A click selects, a double-click
// opens the issue in the panel.

// clickRoadmap selects the clicked roadmap row; a press on its bar starts
// a drag (dragRoadmap).
func (m Model) clickRoadmap(x, y, count int) (tea.Model, tea.Cmd) {
	r := m.jiraTab.roadmap
	line := y - jiraBodyTop // the timeline's header first
	i := r.top + line - 1
	if line < 1 || i >= len(r.rows()) {
		return m, nil
	}
	r.idx = i
	m.roadmapSayBlockers()
	if roadmapOnFold(r, r.rows()[i], x) {
		m.foldRoadmap()
		return m, nil
	}
	if count == 2 {
		if k := m.roadmapKey(); k != "" {
			return m.openJiraKey(k)
		}
	}
	if row := r.rows()[i]; row.epic >= 0 {
		col := m.roadmapColAt(x)
		if s, t, ok := roadmapBarCols(r.rowEpic(row), r.from, roadmapZooms[r.zoom]); ok && col >= s && col <= t {
			grip := roadmapGripMove
			switch {
			case t-s >= 2 && col == s:
				grip = roadmapGripStart
			case t-s >= 2 && col == t:
				grip = roadmapGripEnd
			}
			key, start, end, fromSprints := r.rowDates(row)
			r.drag = roadmapDrag{on: true, col: col, grip: grip, from: roadmapDates{*start, *end, *fromSprints, r.pending[key]}}
		}
	}
	return m, nil
}

// roadmapOnFold is whether x is on row's ▾/▸: a parent's, or an epic's
// with children.
func roadmapOnFold(r *roadmapState, row roadmapRow, x int) bool {
	at := 1 // the box's left border
	switch {
	case row.epic < 0:
	case row.kid >= 0 || len(r.epics[row.epic].Kids) == 0:
		return false
	case r.epics[row.epic].Parent != "":
		at += 2 // indented under its parent
	}
	return x >= at && x < at+2
}

// Where a roadmap bar is held: the whole bar or one of its ends.
const (
	roadmapGripMove = iota
	roadmapGripStart
	roadmapGripEnd
)

// roadmapDrag is a bar held by the mouse: grip and the column it was last
// at.
type roadmapDrag struct {
	on        bool
	col, grip int
	// from is the held row's dates as they were, for esc to put back.
	from roadmapDates
}

// roadmapDates is a row's dates and whether they were pending a write.
type roadmapDates struct {
	start, end           time.Time
	fromSprints, pending bool
}

// dragRoadmap moves the held bar (or its end) a column's worth of days per
// column the mouse moved; the write waits for a pause, as with the keys.
func (m Model) dragRoadmap(x int) (tea.Model, tea.Cmd) {
	r := m.jiraTab.roadmap
	col := m.roadmapColAt(x)
	d := (col - r.drag.col) * roadmapZooms[r.zoom]
	if d == 0 {
		return m, nil
	}
	r.drag.col = col
	switch r.drag.grip {
	case roadmapGripStart:
		return m, m.shiftRoadmap(d, 0)
	case roadmapGripEnd:
		return m, m.shiftRoadmap(0, d)
	}
	return m, m.shiftRoadmap(d, d)
}

// dragging is whether a mouse drag is in progress.
func (m *Model) dragging() bool {
	r, p := m.jiraTab.roadmap, m.jiraTab.plan
	return m.panelResizing || m.jiraDragging() || r != nil && r.drag.on || p != nil && p.drag.held
}

// cancelDrag drops the drag in progress (esc): nothing is written and the
// dragged thing is back where it was.
func (m Model) cancelDrag() (tea.Model, tea.Cmd) {
	m.status = "drag cancelled"
	switch r, p := m.jiraTab.roadmap, m.jiraTab.plan; {
	case m.panelResizing:
		m.panelResizing = false
		m.opts.panelPct = m.panelResizeFrom
		m.resize()
	case r != nil && r.drag.on:
		from := r.drag.from
		r.drag = roadmapDrag{}
		row, ok := r.selected()
		if !ok || row.epic < 0 {
			return m, nil
		}
		key, start, end, fromSprints := r.rowDates(row)
		*start, *end, *fromSprints = from.start, from.end, from.fromSprints
		if !from.pending {
			delete(r.pending, key)
		}
		r.saveSeq++ // the drag's write is off; others pending get theirs
		if len(r.pending) > 0 {
			seq := r.saveSeq
			return m, tea.Tick(roadmapSaveDelay, func(time.Time) tea.Msg { return roadmapSaveMsg{seq} })
		}
	case p != nil && p.drag.held:
		p.drag = planDrag{}
	case m.jiraDragging():
		m.jiraTab.drag = jiraDrag{}
		m.renderJira()
	}
	return m, nil
}

// roadmapColAt is the timeline column under screen column x.
func (m *Model) roadmapColAt(x int) int {
	labelW, _ := roadmapLayout(m.jiraTab.view.Width())
	return x - 1 - labelW - 1 // the box's border, the labels and a space
}

// planSideAt is the planning side under column x: 0 the backlog, 1 the
// sprint.
func (m *Model) planSideAt(x int) int {
	if leftW := (m.jiraTab.view.Width() - 3) / 2; x-1 < leftW+2 {
		return 0
	}
	return 1
}

// clickPlan selects the clicked card on either side.
func (m Model) clickPlan(x, y, count int) (tea.Model, tea.Cmd) {
	p := m.jiraTab.plan
	side := m.planSideAt(x)
	line := y - jiraBodyTop // the side's head and its per-assignee line first
	i := p.top[side] + line - 2
	switch {
	case line >= 0 && line < 2: // its head focuses the side
		p.side = side
		return m, nil
	case line < 2 || i >= len(p.sides[side]):
		return m, nil
	}
	p.side, p.idx[side] = side, i
	if count == 2 {
		return m.openJiraKey(p.sides[side][i].Key)
	}
	p.drag = planDrag{key: p.sides[side][i].Key, x: x, y: y, side: side, over: side, held: true}
	return m, nil
}

// dragPlan follows a held card: it lifts once the pointer leaves its cell,
// and the side under the pointer is where it would land.
func (m Model) dragPlan(x, y int) (tea.Model, tea.Cmd) {
	d := &m.jiraTab.plan.drag
	if !d.active && x-d.x < 2 && d.x-x < 2 && y == d.y {
		return m, nil
	}
	d.active, d.over = true, m.planSideAt(x)
	if d.over != d.side {
		m.status = "drop " + d.key + " on " + []string{"the backlog", m.jiraTab.plan.sprints[m.jiraTab.plan.target].name}[d.over]
	} else {
		m.status = ""
	}
	return m, nil
}

// dropPlan lets go of a held card: over the other side it moves there,
// taking the side's marked cards along when it is one of them.
func (m Model) dropPlan() (tea.Model, tea.Cmd) {
	p := m.jiraTab.plan
	d := p.drag
	p.drag = planDrag{}
	if !d.active || d.over == d.side {
		return m, nil
	}
	p.side = d.side
	if m.jiraTab.marked[d.key] {
		return m, m.planMove()
	}
	return m, m.planMoveOf(func(c jira.Card) bool { return c.Key == d.key })
}

// wheelView moves the roadmap's or planning's cursor by d rows.
func (m *Model) wheelView(x, d int) {
	t := m.jiraTab
	switch {
	case t.roadmap != nil:
		r := t.roadmap
		r.idx = min(max(r.idx+d, 0), max(len(r.rows())-1, 0))
	case t.plan != nil:
		p := t.plan
		side := m.planSideAt(x)
		p.side = side
		p.idx[side] = min(max(p.idx[side]+d, 0), max(len(p.sides[side])-1, 0))
	}
}
