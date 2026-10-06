package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
)

// TestJiraLaneLayout: alt+l switches to a fitting ui.lane_layouts entry,
// which stacks To do and In progress in one lane under their section
// headers, keeps Done after it and is remembered for the board; alt+l
// again goes back to the board's columns.
func TestJiraLaneLayout(t *testing.T) {
	m := jiraTabModel(t)
	m.opts.laneLayouts = []config.LaneLayout{
		{Name: "Elsewhere", Boards: []int{2}, Lanes: []config.LaneSpec{{Statuses: []string{"1", "3"}}}},
		{Name: "Flow", Lanes: []config.LaneSpec{{Name: "Work", Statuses: []string{"3", "1"}}}},
	}
	out, _ := m.handleJiraKey(keyMsg(t, "alt+l"))
	m = out.(Model)
	ls := m.jiraTab.lanes
	if len(ls) != 2 || ls[0].name != "Work" || ls[1].name != "Done" {
		t.Fatalf("lanes %+v, want Work and Done", ls)
	}
	var keys []string
	for _, ci := range ls[0].cards {
		keys = append(keys, m.jiraTab.cards[ci].Key)
	}
	if strings.Join(keys, " ") != "ABC-1 ABC-3 ABC-2" {
		t.Errorf("Work's cards %v, want To do's then In progress's", keys)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Work 3", "─ To do 2", "─ In progress 1", "lanes: Flow"} {
		if !strings.Contains(view, want) {
			t.Errorf("board lacks %q:\n%s", want, view)
		}
	}
	if v, _, _ := m.store.GetMeta(jiraLayoutKey(1)); v != "Flow" {
		t.Errorf("remembered %q, want Flow", v)
	}
	// ABC-2's key line: under the header, two cards and the second header.
	y := jiraBodyTop + 1 + 1 + 2*m.cardSlot() + 1
	if h := m.hitJira(2, y); h.idx != 0 || h.line != 2 {
		t.Errorf("hit at y %d: lane %d row %d, want ABC-2 at 0/2", y, h.idx, h.line)
	}
	if h := m.hitJira(2, jiraBodyTop+1); h.line != -1 {
		t.Errorf("a section header hits row %d", h.line)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "alt+l"))
	if m = out.(Model); len(m.jiraTab.lanes) != 3 || m.jiraLayoutName() != "" {
		t.Errorf("alt+l again: %d lanes, layout %q; want the board's 3", len(m.jiraTab.lanes), m.jiraLayoutName())
	}
}

// TestJiraLaneLayoutHides: a layout's hidden columns leave the board, and
// the header counts their cards.
func TestJiraLaneLayoutHides(t *testing.T) {
	m := jiraTabModel(t)
	m.opts.laneLayouts = []config.LaneLayout{{Name: "Open", Hidden: []string{"5", "6"}, Lanes: []config.LaneSpec{{Statuses: []string{"1"}}, {Statuses: []string{"3"}}}}}
	out, _ := m.handleJiraKey(keyMsg(t, "alt+l"))
	m = out.(Model)
	if n := len(m.jiraTab.lanes); n != 2 {
		t.Fatalf("%d lanes, want 2", n)
	}
	if s := ansi.Strip(m.View().Content); !strings.Contains(s, "lanes: Open, 1 hidden") {
		t.Errorf("header lacks the hidden count:\n%s", s)
	}
}

// TestJiraLaneLayoutNone: with no layout fitting, alt+l says so.
func TestJiraLaneLayoutNone(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "alt+l"))
	if m = out.(Model); !strings.Contains(m.status, "no lane layout") {
		t.Errorf("status %q", m.status)
	}
}

// TestJiraLaneSections: z folds the cursor's section of a stacked lane to
// its header, the cursor moving on; a click on the header unfolds it; the
// folds are remembered per board, and Z clears them.
func TestJiraLaneSections(t *testing.T) {
	m := jiraTabModel(t)
	m.opts.laneLayouts = []config.LaneLayout{{Name: "Flow", Lanes: []config.LaneSpec{{Name: "Work", Statuses: []string{"1", "3"}}}}}
	out, _ := m.handleJiraKey(keyMsg(t, "alt+l"))
	m = out.(Model)
	m.selectJiraKey("ABC-1")
	out, _ = m.handleJiraKey(keyMsg(t, "z"))
	m = out.(Model)
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-2" {
		t.Errorf("cursor on %s, want ABC-2 past the folded To do", c.Key)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "▸ To do 2") || strings.Contains(view, "ABC-3") {
		t.Errorf("To do should fold to its header:\n%s", view)
	}
	if v, _, _ := m.store.GetMeta(jiraSecFoldKey(1)); v != secFoldKey("Work", "To do") {
		t.Errorf("remembered %q", v)
	}
	h := m.hitJira(2, jiraBodyTop+1)
	if h.section != 1 || h.idx != 0 {
		t.Fatalf("header hit %+v", h)
	}
	out, _ = m.clickJira(h, 2, jiraBodyTop+1, 1)
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "ABC-3") {
		t.Errorf("a click should unfold To do:\n%s", view)
	}
	m.toggleJiraSection(0, 1)
	out, _ = m.handleJiraKey(keyMsg(t, "Z"))
	if m = out.(Model); len(m.jiraTab.secFold) != 0 {
		t.Errorf("Z left folds %v", m.jiraTab.secFold)
	}
}
