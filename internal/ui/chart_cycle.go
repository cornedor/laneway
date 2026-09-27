package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// The charts' Cycle tab: how long work takes. Each issue resolved in the
// last cycleWeeks is a dot at the day it was done and its cycle time (first
// in progress to done), under the 50th and 85th percentile lines; lead
// time (created to done) is summed up beside it, and the slowest listed.

const cycleWeeks = 8

// cycleDays is d in days, one decimal.
func cycleDays(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(24*time.Hour), 'f', 1, 64)
}

func renderCycle(issues []jira.CycleIssue, now time.Time, width, height int) string {
	if len(issues) == 0 {
		return refDimStyle.Render(fmt.Sprintf("nothing resolved in the last %d weeks", cycleWeeks))
	}
	var cycles, leads []time.Duration
	for _, ci := range issues {
		if ci.Cycle > 0 {
			cycles = append(cycles, ci.Cycle)
		}
		leads = append(leads, ci.Lead)
	}
	p50, p85 := jira.Percentile(cycles, 50), jira.Percentile(cycles, 85)
	lines := []string{
		jiraViewActive.Render("Cycle time") + jiraDimStyle.Render(fmt.Sprintf("  %d resolved in %d weeks · in progress to done: 50%% within %s days, 85%% within %s",
			len(issues), cycleWeeks, cycleDays(p50), cycleDays(p85))),
		jiraDimStyle.Render(fmt.Sprintf("lead time, created to done: 50%% within %s days, 85%% within %s", cycleDays(jira.Percentile(leads, 50)), cycleDays(jira.Percentile(leads, 85)))),
		"",
	}
	// The scatter: rows are days, top the slowest shown; columns days back.
	rows := max(min(height-12, 14), 4)
	cols := max(width-8, 10)
	top := max(p85+p85/2, time.Hour)
	for _, c := range cycles {
		top = max(top, min(c, 3*p85))
	}
	grid := make([][]rune, rows)
	for r := range grid {
		grid[r] = []rune(strings.Repeat(" ", cols))
	}
	from := now.AddDate(0, 0, -7*cycleWeeks)
	rowOf := func(d time.Duration) int { return rows - 1 - min(int(float64(rows-1)*float64(d)/float64(top)), rows-1) }
	for _, pd := range []time.Duration{p50, p85} {
		if pd > 0 {
			for c := range grid[rowOf(pd)] {
				grid[rowOf(pd)][c] = '┄'
			}
		}
	}
	for _, ci := range issues {
		if ci.Cycle == 0 {
			continue
		}
		c := min(int(float64(cols-1)*float64(ci.Resolved.Sub(from))/float64(now.Sub(from))), cols-1)
		grid[rowOf(ci.Cycle)][max(c, 0)] = '●'
	}
	for r, g := range grid {
		label := "      "
		switch r {
		case 0:
			label = fmt.Sprintf("%5sd", cycleDays(top))
		case rowOf(p85):
			label = "  85%"
		case rowOf(p50):
			label = "  50%"
		case rows - 1:
			label = "    0d"
		}
		lines = append(lines, jiraDimStyle.Render(fmt.Sprintf("%-6s ", label))+roadmapTodoStyle.Render(string(g)))
	}
	lines = append(lines, jiraDimStyle.Render(fmt.Sprintf("       %s%*s", from.Format("2 Jan"), cols-6, "today")), "")
	slow := slices.Clone(issues)
	slices.SortFunc(slow, func(a, b jira.CycleIssue) int { return int(b.Cycle - a.Cycle) })
	lines = append(lines, jiraDimStyle.Render("slowest"))
	for _, ci := range slow[:min(len(slow), max(height-len(lines)-1, 0), 5)] {
		lines = append(lines, fmt.Sprintf("  %s  %s days  %s", jiraKeyStyle.Render(ci.Key), cycleDays(ci.Cycle), truncate(ci.Summary, max(width-30, 10))))
	}
	return strings.Join(lines, "\n")
}
