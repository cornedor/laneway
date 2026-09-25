package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

func TestBurnSeries(t *testing.T) {
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 14)
	now := start.AddDate(0, 0, 2).Add(time.Hour)
	issues := []jira.BurnIssue{
		{Points: 5, Resolved: start.Add(2 * time.Hour)},
		{Points: 3, Resolved: start.AddDate(0, 0, 2)},
		{Points: 2},
	}
	total, added, left := burnSeries(issues, start, end, now)
	if total != 10 || added != 0 || !slices.Equal(left, []float64{5, 5, 2}) {
		t.Errorf("total %v, added %v, left %v", total, added, left)
	}
	// An issue added on day 2 counts from then.
	issues = append(issues, jira.BurnIssue{Points: 4, Added: start.AddDate(0, 0, 1).Add(time.Hour)})
	total, added, left = burnSeries(issues, start, end, now)
	if total != 14 || added != 4 || !slices.Equal(left, []float64{5, 9, 6}) {
		t.Errorf("with scope: total %v, added %v, left %v", total, added, left)
	}
}

// chartsModel has the active sprint dated and the charts open with data.
func chartsModel(t *testing.T) Model {
	t.Helper()
	m := jiraTabModel(t)
	now := time.Now()
	m.jiraTab.views[0].start, m.jiraTab.views[0].end = now.AddDate(0, 0, -3), now.AddDate(0, 0, 7)
	out, cmd := m.handleJiraKey(keyMsg(t, "C"))
	m = out.(Model)
	if m.jiraTab.charts == nil || cmd == nil {
		t.Fatal("C should open the charts and load them")
	}
	out, _ = m.handleCharts(chartsMsg{seq: m.jiraTab.charts.seq,
		burn: []jira.BurnIssue{{Points: 8, Resolved: now.AddDate(0, 0, -1)}, {Points: 5}},
		vel:  []jira.SprintVelocity{{Name: "Sprint 0", Committed: 20, Done: 15}, {Name: "Sprint -1", Committed: 18, Done: 18}},
	})
	return out.(Model)
}

func TestChartsView(t *testing.T) {
	m := chartsModel(t)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Burndown", "Sprint 1  5 of 13p left", "13"} {
		if !strings.Contains(view, want) {
			t.Errorf("burndown lacks %q", want)
		}
	}
	if !strings.ContainsFunc(view, func(r rune) bool { return r > 0x2800 && r <= 0x28ff }) {
		t.Error("no plot drawn")
	}
	out, _ := m.handleJiraKey(keyMsg(t, "tab"))
	m = out.(Model)
	view = ansi.Strip(m.View().Content)
	for _, want := range []string{"last 2 sprints · average 16.5p done", "Sprint 0", "15/20", "18/18", "█"} {
		if !strings.Contains(view, want) {
			t.Errorf("velocity lacks %q", want)
		}
	}
	out, _ = m.handleJiraKey(keyMsg(t, "esc"))
	if m = out.(Model); m.jiraTab.charts != nil {
		t.Error("esc should bring the board back")
	}
}
