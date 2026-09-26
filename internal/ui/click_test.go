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
