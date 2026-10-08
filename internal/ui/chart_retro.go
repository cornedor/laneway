package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// The charts' Retro tab: the last closed sprint beside the one before —
// what was committed, added during it, done, carried over and moved
// backwards — and which issues those were.

// retroTable is the comparison as rows: a metric, then a column a sprint.
func retroTable(rs []jira.RetroSprint, ln chartLines) ([]string, [][]string) {
	head := []string{""}
	for _, r := range rs {
		head = append(head, r.Name)
	}
	metric := func(name string, f func(r jira.RetroSprint) string) []string {
		row := []string{name}
		for _, r := range rs {
			row = append(row, f(r))
		}
		return row
	}
	n := func(keys []string) string { return strconv.Itoa(len(keys)) }
	rows := [][]string{
		metric(i18n.T("committed"), func(r jira.RetroSprint) string { return n(r.Committed) }),
		metric(i18n.T("added during"), func(r jira.RetroSprint) string { return n(r.Added) }),
		metric(i18n.T("done"), func(r jira.RetroSprint) string { return n(r.Done) }),
		metric(i18n.T("carried over"), func(r jira.RetroSprint) string { return n(r.Carried) }),
		metric(i18n.T("moved backwards"), func(r jira.RetroSprint) string { return n(r.Back) }),
		metric(i18n.T("points done"), func(r jira.RetroSprint) string {
			return i18n.Tf("%s of %s", chartNum(r.DonePoints), chartNum(r.Points))
		}),
	}
	if ln.compare != nil {
		rows = append(rows, metric(betweenName(ln), func(r jira.RetroSprint) string { return n(r.Between) }))
	}
	return head, rows
}

// betweenName names the issues past the left of two lines, not the right.
func betweenName(ln chartLines) string {
	first, last := jira.Order(ln.done, ln.compare)
	return i18n.Tf("past %s, not %s", first.Label(), last.Label())
}

func renderRetro(rs []jira.RetroSprint, ln chartLines, width int) string {
	if len(rs) == 0 {
		return refDimStyle.Render(i18n.T("no closed sprints yet"))
	}
	last := rs[len(rs)-1]
	head, rows := retroTable(rs, ln)
	lines := []string{jiraViewActive.Render(i18n.Tf("Retro — %s", last.Name)) + jiraDimStyle.Render(fmt.Sprintf("  %s – %s · %s", last.Start.Local().Format("2 Jan"), last.End.Local().Format("2 Jan"), ln.by())), ""}
	colW, nameW := 14, 18
	for _, r := range rows {
		nameW = max(nameW, len(r[0])+2)
	}
	cell := func(s string) string { return fmt.Sprintf("%-*s", colW, truncate(s, colW-1)) }
	h := strings.Repeat(" ", nameW)
	for _, c := range head[1:] {
		h += cell(c)
	}
	lines = append(lines, jiraDimStyle.Render(strings.Repeat(" ", 2)+h))
	for _, r := range rows {
		line := "  " + fmt.Sprintf("%-*s", nameW, r[0])
		for i, c := range r[1:] {
			if i == len(r)-2 {
				line += jiraKeyStyle.Render(cell(c))
			} else {
				line += jiraDimStyle.Render(cell(c))
			}
		}
		lines = append(lines, line)
	}
	list := func(name string, keys []string) {
		if len(keys) == 0 {
			return
		}
		lines = append(lines, "", jiraDimStyle.Render(name)+"  "+truncate(strings.Join(keys, " "), max(width-len(name)-4, 10)))
	}
	list(i18n.T("carried over"), last.Carried)
	list(i18n.T("added during"), last.Added)
	list(i18n.T("moved backwards"), last.Back)
	if ln.compare != nil {
		list(betweenName(ln), last.Between)
	}
	return strings.Join(lines, "\n")
}
