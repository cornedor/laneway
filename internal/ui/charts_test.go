package ui

import (
	"fmt"
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

// TestBurnSeriesLocalDays: days break at local midnight, whatever zone
// Jira's timestamps carry.
func TestBurnSeriesLocalDays(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("JST", 9*3600)
	t.Cleanup(func() { time.Local = local })
	start := time.Date(2026, 9, 13, 23, 0, 0, 0, time.UTC) // Mon 14 Sep 08:00 JST
	resolved := start.Add(90 * time.Minute)                // Mon 09:30 JST, Mon 00:30 UTC
	_, _, left := burnSeries([]jira.BurnIssue{{Points: 3, Resolved: resolved}, {Points: 1}}, start, start.AddDate(0, 0, 4), start.AddDate(0, 0, 1))
	if !slices.Equal(left, []float64{1, 1}) {
		t.Errorf("left %v, want the resolution on day 0", left)
	}
	if d := localDay(start); d.Format("Mon 2 Jan") != "Mon 14 Sep" {
		t.Errorf("day 0 = %s", d.Format("Mon 2 Jan"))
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
	if view = ansi.Strip(m.View().Content); !strings.Contains(view, "8 of 13p done · scope dotted") {
		t.Errorf("burnup lacks its title:\n%s", view)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "tab"))
	m = out.(Model)
	if view = ansi.Strip(m.View().Content); !strings.Contains(view, "issues per column") || !strings.Contains(view, "█ Done") {
		t.Errorf("flow lacks its legend:\n%s", view)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "tab"))
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

func TestBurnupSeries(t *testing.T) {
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.Local)
	issues := []jira.BurnIssue{
		{Points: 5, Resolved: start.Add(2 * time.Hour)},
		{Points: 3, Added: start.AddDate(0, 0, 1).Add(time.Hour)},
	}
	scope, done := burnupSeries(issues, start, start.AddDate(0, 0, 14), start.AddDate(0, 0, 1).Add(2*time.Hour))
	if !slices.Equal(scope, []float64{5, 8}) || !slices.Equal(done, []float64{5, 5}) {
		t.Errorf("scope %v done %v", scope, done)
	}
}

func TestFlowSeries(t *testing.T) {
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.Local)
	cols := []jira.Column{{Name: "To do", StatusIDs: []string{"1"}}, {Name: "Doing", StatusIDs: []string{"3"}}, {Name: "Done", StatusIDs: []string{"5"}}}
	issues := []jira.BurnIssue{
		{Status: "5", Moves: []jira.StatusMove{{When: start.Add(time.Hour), From: "1", To: "3"}, {When: start.AddDate(0, 0, 1), From: "3", To: "5"}}},
		{Status: "1"},
	}
	got := flowSeries(issues, cols, start, start.AddDate(0, 0, 14), start.AddDate(0, 0, 1).Add(2*time.Hour))
	if len(got) != 2 || !slices.Equal(got[0], []int{1, 1, 0}) || !slices.Equal(got[1], []int{1, 0, 1}) {
		t.Errorf("flow = %v", got)
	}
}

// TestChartsCopy: y copies the open chart's numbers as a markdown table.
func TestChartsCopy(t *testing.T) {
	m := chartsModel(t)
	out, cmd := m.handleKey(keyMsg(t, "y"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("y copied nothing")
	}
	// Burndown: header, rule and a row per day since the start (4 days).
	got := fmt.Sprint(cmd())
	if !strings.HasPrefix(got, "| Day | Points left |\n|---|---|\n") || strings.Count(got, "\n") != 6 || !strings.Contains(got, "| 5 |") {
		t.Errorf("burndown table = %q", got)
	}
	m.jiraTab.charts.tab = chartVelocity
	_, cmd = m.handleKey(keyMsg(t, "y"))
	if got := fmt.Sprint(cmd()); !strings.Contains(got, "| Sprint 0 | 20 | 15 |") {
		t.Errorf("velocity table = %q", got)
	}
}

// TestChartsPlanStaleAfterReopen: a reply for charts or planning closed
// before it loaded doesn't fill the one reopened.
func TestChartsPlanStaleAfterReopen(t *testing.T) {
	m := jiraTabModel(t)
	for _, k := range []string{"C", "P"} {
		out, _ := m.handleJiraKey(keyMsg(t, k))
		m = out.(Model)
		var stale int
		if k == "C" {
			stale = m.jiraTab.charts.seq
		} else {
			stale = m.jiraTab.plan.seq
		}
		out, _ = m.handleJiraKey(keyMsg(t, "esc"))
		m = out.(Model)
		if m.jiraTab.charts != nil || m.jiraTab.plan != nil {
			t.Fatalf("%s: esc should close", k)
		}
		out, _ = m.handleJiraKey(keyMsg(t, k))
		m = out.(Model)
		if k == "C" {
			out, _ = m.handleCharts(chartsMsg{seq: stale, vel: []jira.SprintVelocity{{Name: "Old board"}}})
			if m = out.(Model); !m.jiraTab.charts.loading || m.jiraTab.charts.vel != nil {
				t.Error("C: a stale reply filled the reopened charts")
			}
		} else {
			out, _ = m.handlePlan(planMsg{seq: stale, left: []jira.Card{{Key: "OLD-1"}}})
			if m = out.(Model); !m.jiraTab.plan.loading || m.jiraTab.plan.sides[0] != nil {
				t.Error("P: a stale reply filled the reopened planning")
			}
		}
		out, _ = m.handleJiraKey(keyMsg(t, "esc"))
		m = out.(Model)
	}
}

// TestCycleChart: the cycle tab plots resolved issues with their
// percentiles, lists the slowest, and copies the numbers.
func TestCycleChart(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.Local)
	day := 24 * time.Hour
	issues := []jira.CycleIssue{
		{Key: "ABC-1", Summary: "Quick", Resolved: now.Add(-20 * day), Cycle: day, Lead: 3 * day},
		{Key: "ABC-2", Summary: "Slow", Resolved: now.Add(-5 * day), Cycle: 9 * day, Lead: 20 * day},
		{Key: "ABC-3", Summary: "Never started", Resolved: now.Add(-2 * day), Lead: day},
	}
	out := ansi.Strip(renderCycle(issues, now, 100, 30))
	for _, want := range []string{"3 resolved in 8 weeks", "50% within 1.0 days, 85% within 9.0", "●", "85%", "slowest", "ABC-2  9.0 days  Slow"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	m := jiraTabModel(t)
	m.jiraTab.charts = &chartsState{tab: chartCycle, cycle: issues}
	if tbl := m.chartTable(now); !strings.Contains(tbl, "| ABC-2 |") || !strings.Contains(tbl, "| 9.0 | 20.0 |") {
		t.Errorf("table:\n%s", tbl)
	}
}

// TestRetroChart: the last sprint beside the one before, with the issues
// carried over and moved back, and as a table.
func TestRetroChart(t *testing.T) {
	rs := []jira.RetroSprint{
		{Name: "S1", Committed: []string{"A-1", "A-2"}, Done: []string{"A-1", "A-2"}, Points: 5, DonePoints: 5},
		{Name: "S2", Committed: []string{"A-3"}, Added: []string{"A-4"}, Done: []string{"A-3"}, Carried: []string{"A-4"}, Back: []string{"A-3"}, Points: 8, DonePoints: 3},
	}
	out := ansi.Strip(renderRetro(rs, 100))
	for _, want := range []string{"Retro — S2", "S1", "carried over      0             1", "moved backwards   0", "points done       5 of 5        3 of 8", "carried over  A-4", "moved backwards  A-3"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	m := jiraTabModel(t)
	m.jiraTab.charts = &chartsState{tab: chartRetro, retro: rs}
	if tbl := m.chartTable(time.Now()); !strings.Contains(tbl, "| carried over | 0 | 1 |") {
		t.Errorf("table:\n%s", tbl)
	}
}

// TestBurndownPace: the title says where the sprint stands against the
// ideal, and today is marked under the chart.
func TestBurndownPace(t *testing.T) {
	start := time.Now().AddDate(0, 0, -7)
	v := jiraView{name: "Sprint 1", start: start, end: start.AddDate(0, 0, 14)}
	issues := []jira.BurnIssue{{Points: 10}, {Points: 10}} // half the sprint gone, nothing done
	got := ansi.Strip(renderBurndown(v, issues, time.Now(), 80, 20))
	for _, want := range []string{"20 of 20p left · ideal 10p · 10p behind", "⣿ left", "⠉ ideal", "▲ today"} {
		if !strings.Contains(got, want) {
			t.Errorf("burndown lacks %q:\n%s", want, got)
		}
	}
	issues[0].Resolved, issues[1].Resolved = start.Add(time.Hour), start.Add(time.Hour)
	if got := ansi.Strip(renderBurndown(v, issues, time.Now(), 80, 20)); !strings.Contains(got, "10p ahead") {
		t.Errorf("all done early:\n%s", got)
	}
}
