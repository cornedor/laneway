package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/herdr"
)

// TestCardRightClick: a right-click on a card selects it and opens its quick
// actions; a field edits that card, a key row presses the key on it.
func TestCardRightClick(t *testing.T) {
	for _, lanes := range []bool{true, false} {
		m := jiraTabModel(t)
		if !lanes {
			m.jiraTab.wantLanes = false
			m.buildJiraLanes()
			m.renderJira()
		}
		x, y := screenAt(t, m, "ABC-3")
		out, _ := m.handleClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
		m = out.(Model)
		if c, ok := m.selectedJiraCard(); !ok || c.Key != "ABC-3" {
			t.Fatalf("lanes %v: selected %v, want ABC-3", lanes, c.Key)
		}
		if !m.jiraPicker.active || m.jiraPicker.title != "Edit ABC-3" || m.quickKey != "ABC-3" {
			t.Fatalf("lanes %v: menu %q (quick %q)", lanes, m.jiraPicker.title, m.quickKey)
		}
		var labels []string
		for _, it := range m.jiraPicker.items {
			labels = append(labels, strings.TrimSpace(ansi.Strip(it.label)+"  "+it.hint))
		}
		if got := strings.Join(labels, "|"); !strings.Contains(got, "Assignee") || !strings.Contains(got, "Copy the key  y") {
			t.Fatalf("items %s", got)
		}
		menu := m
		menu.jiraPicker.idx = 2 // assignee
		out, _ = menu.handleJiraPickerKey(keyPress("enter"))
		if a := out.(Model); a.jiraPicker.kind != jiraPickAssignee || a.jiraPicker.issueKey != "ABC-3" {
			t.Errorf("lanes %v: assignee picker for %q (kind %v)", lanes, a.jiraPicker.issueKey, a.jiraPicker.kind)
		}
		m.jiraPicker.idx = len(m.jiraPicker.items) - 2 // copy the key
		out, _ = m.handleJiraPickerKey(keyPress("enter"))
		if m = out.(Model); m.jiraPicker.active || m.quickKey != "" || !strings.Contains(m.status, "ABC-3") {
			t.Errorf("lanes %v: copy: status %q, quick %q", lanes, m.status, m.quickKey)
		}
	}
}

// A right-click off the cards, or with a menu open, does nothing.
func TestCardRightClickElsewhere(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleClick(tea.MouseClickMsg{X: 5, Y: 0, Button: tea.MouseRight})
	if m = out.(Model); m.jiraPicker.active {
		t.Error("a right-click on the header opened a menu")
	}
}

// TestAgentRowsClick: in the panel an agent's rows attach to it, and the
// heading's hints press their keys.
func TestAgentRowsClick(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	herdrBin = "true"
	t.Cleanup(func() { herdrBin = "herdr" })
	m := loadedJiraModel(t)
	m.width = 200
	m.resize()
	m.herdr = herdr.New("/nowhere.sock")
	out, _ := m.handleAgents(agentsMsg{agents: []herdr.Agent{
		{PaneID: "p1", Name: "jira-abc-1-a", Status: herdr.Idle, Title: "thinking"},
		{PaneID: "p2", Name: "jira-abc-1-b", Status: herdr.Working},
	}})
	m = out.(Model)
	m.renderRef()
	for _, row := range []string{"jira-abc-1-b", "thinking"} {
		x, y := screenAt(t, m, row)
		out, cmd := m.handleClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if mm := out.(Model); cmd == nil || !strings.Contains(mm.status, "attached") {
			t.Errorf("click on %q: status %q", row, mm.status)
		}
		if hovered(m, x, y) == "" {
			t.Errorf("no hover on %q", row)
		}
	}
	x, y := screenAt(t, m, "A more")
	if got := hovered(m, x, y); got != "A more" {
		t.Errorf("hover on the hint = %q", got)
	}
	out, _ = m.handleClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if mm := out.(Model); !mm.jiraPicker.active {
		t.Error("A more should open the issue actions")
	}
}

// TestCardMenuDropdown: the menu opens at the pointer over the board; a row
// follows the pointer and a click picks it; Assignee opens its list beside
// the row, the menu still drawn, esc back to it; a click off both closes.
func TestCardMenuDropdown(t *testing.T) {
	m := jiraTabModel(t)
	x, y := screenAt(t, m, "ABC-3")
	out, _ := m.handleClick(tea.MouseClickMsg{X: x + 2, Y: y, Button: tea.MouseRight})
	m = out.(Model)
	sx, sy := screenAt(t, m, "Status")
	if sy != y+1 || sx != x+4 {
		t.Errorf("Status at %d,%d, want the menu's corner at the pointer %d,%d", sx, sy, x+2, y)
	}
	if _, fy := screenAt(t, m, "First"); fy >= y {
		t.Error("the board should show round the menu")
	}
	ax, ay := screenAt(t, m, "Assignee")
	out, _ = m.Update(tea.MouseMotionMsg{X: ax, Y: ay})
	if m = out.(Model); m.jiraPicker.idx != 2 {
		t.Errorf("hovered row %d, want Assignee (2)", m.jiraPicker.idx)
	}
	out, _ = m.handleClick(tea.MouseClickMsg{X: ax, Y: ay, Button: tea.MouseLeft})
	m = out.(Model)
	if m.jiraPicker.kind != jiraPickAssignee || m.jiraPicker.at == nil || m.dropdown == nil || m.dropdown.parent == "" {
		t.Fatalf("assignee should open as a submenu: kind %v at %v", m.jiraPicker.kind, m.jiraPicker.at)
	}
	if _, fy := screenAt(t, m, "filter…"); fy != ay {
		t.Errorf("the submenu's filter at row %d, want beside Assignee (%d)", fy, ay)
	}
	screenAt(t, m, "Story points") // the menu, still drawn
	out, _ = m.handleJiraPickerKey(keyPress("esc"))
	if m = out.(Model); m.jiraPicker.kind != jiraPickBulk || m.jiraPicker.idx != 2 || m.dropdown.parent != "" {
		t.Fatalf("esc should go back to the menu on Assignee: kind %v idx %d", m.jiraPicker.kind, m.jiraPicker.idx)
	}
	out, _ = m.handleClick(tea.MouseClickMsg{X: m.width - 3, Y: m.bodyH() - 2, Button: tea.MouseLeft})
	if m = out.(Model); m.jiraPicker.active || m.dropdown != nil {
		t.Error("a click off the menu should close it")
	}
}
