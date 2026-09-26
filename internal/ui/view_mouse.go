package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Clicks and the wheel in the views that take the board's place: the
// roadmap's rows and planning's two sides. A click selects, a double-click
// opens the issue in the panel.

// clickRoadmap selects the clicked roadmap row.
func (m Model) clickRoadmap(y, count int) (tea.Model, tea.Cmd) {
	r := m.jiraTab.roadmap
	line := y - jiraBodyTop // the timeline's header first
	i := r.top + line - 1
	if line < 1 || i >= len(r.rows()) {
		return m, nil
	}
	r.idx = i
	m.roadmapSayBlockers()
	if count == 2 {
		if k := m.roadmapKey(); k != "" {
			return m.openJiraKey(k)
		}
	}
	return m, nil
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
	if line < 2 || i >= len(p.sides[side]) {
		return m, nil
	}
	p.side, p.idx[side] = side, i
	if count == 2 {
		return m.openJiraKey(p.sides[side][i].Key)
	}
	return m, nil
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

// Header clicks: the view line's names switch view, the filter line's
// assignee chip opens its picker and a quick filter's chip toggles it.

// headerHit is what the header cell x, y does: kind "view" or "quick"
// with its index, "assignee", or "" for nothing.
func (m *Model) headerHit(x, y int) (kind string, i int) {
	t := m.jiraTab
	if t.roadmap != nil || t.plan != nil || t.charts != nil {
		return "", 0
	}
	at := 1 // the box's left border
	span := func(s string) bool {
		w := ansi.StringWidth(s)
		hit := x >= at && x < at+w
		at += w
		return hit
	}
	switch y {
	case jiraBodyTop - 2: // the views
		for i, v := range t.views {
			if i > 0 {
				span("  │  ")
			}
			if span(v.name) {
				return "view", i
			}
		}
	case jiraBodyTop - 1: // the filters
		switch {
		case t.searching:
			return "", 0
		case t.jiraSearchQuery() != "":
			span("/" + t.search.Value() + " esc  ")
		}
		who := "everyone"
		if t.assignee.id != "" {
			who = t.assignee.label
		}
		span(helpKey(m.keys.Assignee) + " assignee (" + helpKey(m.keys.Mine) + " me): ")
		if span(who) {
			return "assignee", 0
		}
		for i, q := range t.quick {
			if i == 9 {
				break
			}
			span("  ")
			if span(strconv.Itoa(i+1) + " " + q.Name) {
				return "quick", i
			}
		}
	}
	return "", 0
}

// clickHeader acts on a header hit; ok false when x, y is none.
func (m Model) clickHeader(x, y int) (tea.Model, tea.Cmd, bool) {
	kind, i := m.headerHit(x, y)
	switch kind {
	case "view":
		return m, m.cycleJiraView(i - m.jiraTab.viewIdx), true
	case "quick":
		return m, m.toggleJiraQuick(i), true
	case "assignee":
		out, cmd := m.handleJiraKey(keyPress(helpKey(m.keys.Assignee)))
		return out, cmd, true
	}
	return m, nil, false
}
