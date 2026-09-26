package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// viewLineOf is the screen row of the first line containing s, -1 for none.
func viewLineOf(m Model, s string) int {
	for i, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func click(m Model, x, y int) Model {
	out, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return out.(Model)
}

// TestClickSwimlaneHeader: a click on a band's header folds it, again
// unfolds it.
func TestClickSwimlaneHeader(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "s"))
	m = out.(Model)
	y := viewLineOf(m, "▾ AD Ada")
	if y < 0 {
		t.Fatalf("no Ada band:\n%s", ansi.Strip(m.View().Content))
	}
	if m = click(m, 5, y); viewLineOf(m, "▸ AD Ada · 1") < 0 || viewLineOf(m, "First") >= 0 {
		t.Fatalf("click did not fold:\n%s", ansi.Strip(m.View().Content))
	}
	if c, _ := m.selectedJiraCard(); c.Key == "ABC-1" {
		t.Error("the cursor stayed in the fold")
	}
	if m = click(m, 5, viewLineOf(m, "▸ AD Ada")); viewLineOf(m, "First") < 0 {
		t.Error("click again should unfold")
	}
}

// TestClickAboveBoard: a click on the header's blank space leaves the
// cursor's lane alone.
func TestClickAboveBoard(t *testing.T) {
	m := jiraTabModel(t)
	lane := m.jiraTab.lane
	if m = click(m, m.jiraTab.laneW+3, 1); m.jiraTab.lane != lane {
		t.Errorf("lane %d, want %d", m.jiraTab.lane, lane)
	}
}

// TestClickClosesHelp: any click closes the help overlay.
func TestClickClosesHelp(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "?"))
	if m = click(out.(Model), 3, 3); m.helpOpen {
		t.Error("help still open")
	}
}

// TestClickChartTab: a click on a chart's name in the view line shows it.
func TestClickChartTab(t *testing.T) {
	m := chartsModel(t)
	line := strings.Split(ansi.Strip(m.View().Content), "\n")[jiraBodyTop-2]
	x := ansi.StringWidth(line[:strings.Index(line, "Flow")])
	if m = click(m, x+1, jiraBodyTop-2); m.jiraTab.charts.tab != 2 {
		t.Errorf("tab %d, want flow", m.jiraTab.charts.tab)
	}
	if m = click(m, 0, jiraBodyTop-2); m.jiraTab.charts.tab != 2 {
		t.Error("a click on the border switched")
	}
}

// clickText clicks the first screen cell showing s, failing when none does.
func clickText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(l, s); i >= 0 {
			return click(m, ansi.StringWidth(l[:i]), y)
		}
	}
	t.Fatalf("no %q on screen:\n%s", s, ansi.Strip(m.View().Content))
	return m
}

// TestClickSettings: a click selects a row, another on it edits, outside
// cancels the edit, then closes.
func TestClickSettings(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, ","))
	m = out.(Model)
	i := 3
	name := m.settings.rows[i].name
	for j, r := range m.settings.rows {
		if j != i && strings.Contains(r.name, name) {
			t.Fatalf("%s is not a unique name", name)
		}
	}
	if m = clickText(t, m, name); m.settings.idx != i || m.settings.input != nil {
		t.Fatalf("idx %d, want %d, not editing", m.settings.idx, i)
	}
	if m = clickText(t, m, name); m.settings.input == nil {
		t.Fatal("a click on the selected row should edit it")
	}
	if m = click(m, 0, 0); m.settings == nil || m.settings.input != nil {
		t.Fatal("outside should cancel the edit only")
	}
	if m = click(m, 0, 0); m.settings != nil {
		t.Error("outside should close")
	}
}

// TestClickFilterBuilder: a field's click moves on to compare, a value's
// adds the term.
func TestClickFilterBuilder(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "F"))
	m = out.(Model)
	if m = clickText(t, m, "Assignee"); m.builderPick(0).id != "assignee" || m.filterBuilder.col != 1 {
		t.Fatalf("field %q col %d", m.builderPick(0).id, m.filterBuilder.col)
	}
	if m = clickText(t, m, "Ada · 1"); m.jiraTab.search.Value() != "assignee:Ada" {
		t.Errorf("query %q", m.jiraTab.search.Value())
	}
	if m = click(m, 0, 0); m.filterBuilder != nil {
		t.Error("outside should close")
	}
}

// TestClickJQL: a click on a completion takes it; outside cancels.
func TestClickJQL(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "Q"))
	m = out.(Model)
	m.jql.input.SetValue("")
	m.jql.sugg = []string{"assignee = currentUser()", "status = Done"}
	if m = clickText(t, m, "status = Done"); m.jql.input.Value() != "status = Done" {
		t.Errorf("input %q", m.jql.input.Value())
	}
	if m = click(m, 0, 0); m.jql != nil {
		t.Error("outside should cancel")
	}
}

// TestWheelOverlay: the wheel moves the settings cursor, not the board.
func TestWheelOverlay(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, ","))
	out, _ = out.(Model).Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if m = out.(Model); m.settings.idx != 1 || m.jiraTab.row != 0 {
		t.Errorf("idx %d, board row %d", m.settings.idx, m.jiraTab.row)
	}
}
