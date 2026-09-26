package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Esc during a drag drops it: nothing is written, the thing is back.

func TestDragCancelCard(t *testing.T) {
	m := jiraTabModel(t)
	laneW, y := m.jiraTab.laneW, jiraBodyTop+1
	out, _ := m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(tea.MouseMotionMsg{X: laneW + 3, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(keyPress("esc"))
	m = out.(Model)
	if m.jiraDragging() || strings.Count(m.jiraTab.lanesOut, "ABC-1") != 1 {
		t.Fatalf("drag = %+v; the ghost should be gone", m.jiraTab.drag)
	}
	out, cmd := m.Update(tea.MouseReleaseMsg{X: laneW + 3, Y: y, Button: tea.MouseLeft})
	if m = out.(Model); cmd != nil || m.jiraTab.lane != 0 {
		t.Error("the release after esc should move nothing")
	}
}

func TestDragCancelPlan(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	y := jiraBodyTop + 3
	right := m.jiraTab.view.Width() - 5
	out, _ := m.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(tea.MouseMotionMsg{X: right, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(keyPress("esc"))
	out, cmd := out.(Model).Update(tea.MouseReleaseMsg{X: right, Y: y, Button: tea.MouseLeft})
	if m = out.(Model); cmd != nil || m.jiraTab.plan.drag.held || len(writes) != 0 {
		t.Errorf("cancelled plan drag wrote: %q", writes)
	}
}

func TestDragCancelRoadmap(t *testing.T) {
	m := roadmapModel(t)
	var writes []string
	fakeRoadmapJira(t, &m, true, &writes)
	r := m.jiraTab.roadmap
	e := &r.epics[0]
	start, end := e.Start, e.End
	s, last, _ := roadmapBarCols(*e, r.from, roadmapZooms[r.zoom])
	labelW, _ := roadmapLayout(m.jiraTab.view.Width())
	x, y := 1+labelW+1+(s+last)/2, jiraBodyTop+1
	out, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(tea.MouseMotionMsg{X: x + 2, Y: y, Button: tea.MouseLeft})
	out, cmd := out.(Model).Update(keyPress("esc"))
	m = out.(Model)
	if !e.Start.Equal(start) || !e.End.Equal(end) || r.pending[e.Key] || cmd != nil || r.drag.on {
		t.Fatalf("after esc: %v – %v, pending %v", e.Start, e.End, r.pending)
	}
	out, _ = m.Update(roadmapSaveMsg{seq: r.saveSeq - 1}) // the drag's own tick
	if len(writes) != 0 {
		t.Errorf("writes = %q", writes)
	}
}

func TestDragCancelPanelResize(t *testing.T) {
	m := loadedJiraModel(t)
	listW, _ := m.jiraListWidth(m.width)
	was, y := m.opts.panelPct, jiraBodyTop+2
	out, _ := m.Update(tea.MouseClickMsg{X: listW, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(tea.MouseMotionMsg{X: m.width / 4, Y: y, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(keyPress("esc"))
	m = out.(Model)
	if m.panelResizing || m.opts.panelPct != was {
		t.Errorf("pct %d resizing %v, want %d and let go", m.opts.panelPct, m.panelResizing, was)
	}
	if nl, _ := m.jiraListWidth(m.width); nl != listW {
		t.Errorf("board %d wide, want %d back", nl, listW)
	}
}
