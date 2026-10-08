package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// The charts' Cycle tab: how long work takes. Each issue done in the last
// cycleWeeks is a dot at the day it was done and its cycle time (first in
// progress to done), under the 50th and 85th percentile lines; lead time
// (created to done) is summed up beside it, and the slowest listed. With a
// second line the cycle runs to the right one, split at the left one.

const cycleWeeks = 8

// cycleDays is d in days, one decimal.
func cycleDays(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(24*time.Hour), 'f', 1, 64)
}

func renderCycle(issues []jira.CycleIssue, ln chartLines, now time.Time, width, height int) string {
	end := ln.done.Label()
	if ln.compare != nil {
		_, last := jira.Order(ln.done, ln.compare)
		end = last.Label()
	}
	if len(issues) == 0 {
		return refDimStyle.Render(i18n.Tf("nothing got to %s in the last %d weeks", end, cycleWeeks))
	}
	var cycles, leads, active, waits []time.Duration
	for _, ci := range issues {
		if ci.Cycle > 0 {
			cycles = append(cycles, ci.Cycle)
			active, waits = append(active, ci.Cycle-ci.Wait), append(waits, ci.Wait)
		}
		leads = append(leads, ci.Lead)
	}
	p50, p85 := jira.Percentile(cycles, 50), jira.Percentile(cycles, 85)
	lines := []string{
		jiraViewActive.Render(i18n.T("Cycle time")) + jiraDimStyle.Render(i18n.Tf("  %d done in %d weeks · in progress to %s: 50%% within %s days, 85%% within %s",
			len(issues), cycleWeeks, end, cycleDays(p50), cycleDays(p85))),
		jiraDimStyle.Render(i18n.Tf("lead time, created to %s: 50%% within %s days, 85%% within %s", end, cycleDays(jira.Percentile(leads, 50)), cycleDays(jira.Percentile(leads, 85)))),
	}
	if ln.compare != nil {
		first, _ := jira.Order(ln.done, ln.compare)
		lines = append(lines, roadmapDoneStyle.Render(i18n.Tf("in progress to %s: 50%% within %s days, 85%% within %s", first.Label(), cycleDays(jira.Percentile(active, 50)), cycleDays(jira.Percentile(active, 85))))+
			jiraDimStyle.Render("  ·  ")+roadmapTodayStyle.Render(i18n.Tf("%s to %s: 50%% within %s, 85%% within %s", first.Label(), end, cycleDays(jira.Percentile(waits, 50)), cycleDays(jira.Percentile(waits, 85)))))
	} else if ln.done != nil {
		lines = append(lines, jiraDimStyle.Render(ln.by()))
	}
	lines = append(lines, "")
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
	lines = append(lines, jiraDimStyle.Render(fmt.Sprintf("       %s%*s", from.Format("2 Jan"), cols-6, i18n.T("today"))), "")
	slow := slices.Clone(issues)
	slices.SortFunc(slow, func(a, b jira.CycleIssue) int { return int(b.Cycle - a.Cycle) })
	lines = append(lines, jiraDimStyle.Render(i18n.T("slowest")))
	for _, ci := range slow[:min(len(slow), max(height-len(lines)-1, 0), 5)] {
		took := i18n.Tf("%s days", cycleDays(ci.Cycle))
		if ln.compare != nil {
			first, _ := jira.Order(ln.done, ln.compare)
			took += jiraDimStyle.Render(i18n.Tf(" (%s after %s)", cycleDays(ci.Wait), first.Label()))
		}
		lines = append(lines, fmt.Sprintf("  %s  %s  %s", jiraKeyStyle.Render(ci.Key), took, truncate(ci.Summary, max(width-40, 10))))
	}
	return strings.Join(lines, "\n")
}
