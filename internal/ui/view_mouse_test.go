package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// clickAt clicks cell x, y, once (the double-click window cleared after).
func clickAt(m Model, x, y int) (Model, tea.Cmd) {
	out, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	m.lastClick.at = time.Time{}
	return m, cmd
}

// TestRoadmapMouse: a click selects a row, a double-click opens its epic,
// the wheel moves the selection.
func TestRoadmapMouse(t *testing.T) {
	m := roadmapModel(t)
	m, _ = clickAt(m, 5, jiraBodyTop+2) // ABC-11, under the header and ABC-10
	if m.jiraTab.roadmap.idx != 1 {
		t.Fatalf("click: idx %d", m.jiraTab.roadmap.idx)
	}
	out, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: jiraBodyTop + 2, Button: tea.MouseWheelUp})
	if m = out.(Model); m.jiraTab.roadmap.idx != 0 {
		t.Errorf("wheel up: idx %d", m.jiraTab.roadmap.idx)
	}
	out, _ = m.Update(tea.MouseClickMsg{X: 5, Y: jiraBodyTop + 1, Button: tea.MouseLeft})
	m = out.(Model)
	out, _ = m.Update(tea.MouseClickMsg{X: 5, Y: jiraBodyTop + 1, Button: tea.MouseLeft})
	if m = out.(Model); !m.refOpen || m.currentRef().jiraKey != "ABC-10" {
		t.Error("double-click should open ABC-10")
	}
}

// TestPlanMouse: a click picks a side and a card on it.
func TestPlanMouse(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	m, _ = clickAt(m, m.jiraTab.view.Width()-5, jiraBodyTop+3) // the sprint's second card
	if p := m.jiraTab.plan; p.side != 1 || p.idx[1] != 1 {
		t.Fatalf("click right: side %d idx %v", p.side, p.idx)
	}
	m, _ = clickAt(m, 3, jiraBodyTop+2)
	if p := m.jiraTab.plan; p.side != 0 || p.idx[0] != 0 {
		t.Errorf("click left: side %d idx %v", p.side, p.idx)
	}
}

// TestPickerMouse: the wheel moves the picker's cursor, a click on a row
// picks it, a click outside the box cancels.
func TestPickerMouse(t *testing.T) {
	m := jiraTabModel(t)
	m.openPalette()
	out, _ := m.Update(tea.MouseWheelMsg{X: 80, Y: 20, Button: tea.MouseWheelDown})
	if m = out.(Model); m.jiraPicker.idx != 1 {
		t.Fatalf("wheel: idx %d", m.jiraPicker.idx)
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	want := m.jiraPicker.items[3].label
	y := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, want) })
	if i, _ := m.pickerRowAt(80, y); i != 3 {
		t.Fatalf("row at y %d = %d, want 3 (%q)", y, i, want)
	}
	m, _ = clickAt(m, 1, 1)
	if m.jiraPicker.active {
		t.Error("a click outside should close the picker")
	}
	m.openPalette()
	for i, it := range m.jiraPicker.items {
		if strings.HasPrefix(it.label, "view  Backlog") {
			m.jiraPicker.idx = i // in the window
			lines = strings.Split(ansi.Strip(m.View().Content), "\n")
			y = slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "view  Backlog") })
			var cmd tea.Cmd
			m, cmd = clickAt(m, 80, y)
			if m.jiraPicker.active || cmd == nil { // the view loads
				t.Errorf("click on row %d: picker %v, cmd %v", i, m.jiraPicker.active, cmd != nil)
			}
			return
		}
	}
	t.Fatal("no Backlog row")
}

// TestLaneWheel: the wheel over another lane scrolls that lane, leaving the
// cursor; over the cursor's lane it moves the cursor.
func TestLaneWheel(t *testing.T) {
	m := bigJiraModel(&testing.B{}, 60)
	laneW := m.jiraTab.laneW
	out, _ := m.Update(tea.MouseWheelMsg{X: laneW + 3, Y: jiraBodyTop + 3, Button: tea.MouseWheelDown})
	if m = out.(Model); m.jiraTab.laneTop[1] != 1 || m.jiraTab.lane != 0 || m.jiraTab.row != 0 {
		t.Errorf("wheel over lane 2: top %v, cursor %d/%d", m.jiraTab.laneTop, m.jiraTab.lane, m.jiraTab.row)
	}
	out, _ = m.Update(tea.MouseWheelMsg{X: 3, Y: jiraBodyTop + 3, Button: tea.MouseWheelDown})
	if m = out.(Model); m.jiraTab.row != 1 {
		t.Errorf("wheel over the cursor's lane: row %d", m.jiraTab.row)
	}
}

// TestHeaderMouse: a click on a view's name switches to it, on a quick
// filter toggles it, on the assignee chip opens its picker.
func TestHeaderMouse(t *testing.T) {
	m := jiraTabModel(t)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	xOf := func(y int, s string) int {
		t.Helper()
		i := strings.Index(lines[y], s)
		if i < 0 {
			t.Fatalf("row %d lacks %q: %q", y, s, lines[y])
		}
		return ansi.StringWidth(lines[y][:i]) + 1
	}
	m, cmd := clickAt(m, xOf(jiraBodyTop-2, "Backlog"), jiraBodyTop-2)
	if cmd == nil {
		t.Error("click on Backlog should load it")
	}
	m, _ = clickAt(m, xOf(jiraBodyTop-1, "1 FE"), jiraBodyTop-1)
	if !m.jiraTab.quickOn[7] {
		t.Error("click on 1 FE should turn it on")
	}
	m, _ = clickAt(m, xOf(jiraBodyTop-1, "everyone"), jiraBodyTop-1)
	if !m.jiraPicker.active {
		t.Error("click on the assignee chip should open its picker")
	}
}

func TestLinkAt(t *testing.T) {
	line := "see " + lipglossBold("x") + osc8Link("https://a.test/1", "\x1b[4mhere\x1b[0m") + " and " + osc8Link("https://b.test", "there")
	for col, want := range map[int]string{0: "", 4: "", 5: "https://a.test/1", 8: "https://a.test/1", 9: "", 14: "https://b.test", 19: ""} {
		if got := linkAt(line, col); got != want {
			t.Errorf("col %d: %q, want %q", col, got, want)
		}
	}
}

func lipglossBold(s string) string { return "\x1b[1m" + s + "\x1b[0m" }

// TestPanelLinkClick: a click on a link in the description opens it (the
// terminal can't, with the mouse captured).
func TestPanelLinkClick(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.openJiraCard()
	m = out.(Model)
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: &jira.Issue{
		Key: "ABC-1", Summary: "First", Description: "Docs at [the spec](https://spec.test/page) for you.",
	}})
	m = out.(Model)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	y := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "the spec") })
	if y < 0 {
		t.Fatal("no link drawn")
	}
	x := ansi.StringWidth(lines[y][:strings.Index(lines[y], "the spec")]) + 2
	m, cmd := clickAt(m, x, y)
	if cmd == nil || m.status != "opening https://spec.test/page…" {
		t.Errorf("click: status %q", m.status)
	}
}

// TestPanelResize: dragging the panel's left border resizes it, within
// bounds; the width is remembered for the next start.
func TestPanelResize(t *testing.T) {
	m := loadedJiraModel(t)
	listW, _ := m.jiraListWidth(m.width)
	y := jiraBodyTop + 2
	row := []rune(ansi.Strip(strings.Split(m.View().Content, "\n")[y]))
	if listW >= len(row) || !strings.ContainsRune("│┃▏", row[listW]) {
		t.Fatalf("no border at column %d: %q", listW, string(row))
	}
	out, _ := m.Update(tea.MouseClickMsg{X: listW, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	out, _ = m.Update(tea.MouseMotionMsg{X: m.width / 4, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.opts.panelPct != 75 {
		t.Fatalf("pct %d after a drag to a quarter, want 75", m.opts.panelPct)
	}
	out, _ = m.Update(tea.MouseMotionMsg{X: 0, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	out, _ = m.Update(tea.MouseReleaseMsg{X: 0, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.opts.panelPct != 80 || m.panelResizing {
		t.Fatalf("pct %d, resizing %v: want clamped to 80 and let go", m.opts.panelPct, m.panelResizing)
	}
	if nl, _ := m.jiraListWidth(m.width); nl >= listW {
		t.Errorf("board %d wide, was %d: the panel should have grown", nl, listW)
	}
	m.opts.panelPct = 50
	m.loadPanelWidth()
	if m.opts.panelPct != 80 {
		t.Errorf("remembered %d, want 80", m.opts.panelPct)
	}
}
