package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// jiraSort orders the list view; s cycles it.
type jiraSort int

const (
	jiraSortRank jiraSort = iota
	jiraSortPriority
	jiraSortPoints
	jiraSortAssignee
	jiraSortEpic
	jiraSortKey
	jiraSortStatus // appended: a stored sort keeps its meaning
	jiraSortUpdated
	jiraSortDue
	jiraSortCreated
	jiraSortCount
)

func (s jiraSort) String() string {
	return [...]string{"rank", "priority", "points", "assignee", "epic", "key", "status", "updated", "due", "created"}[s]
}

// jiraPriorityRank orders priorities highest first; unknown ones sit with
// medium.
func jiraPriorityRank(p string) int {
	switch strings.ToLower(p) {
	case "highest", "blocker", "critical":
		return 0
	case "high", "major":
		return 1
	case "low", "minor":
		return 3
	case "lowest", "trivial":
		return 4
	}
	return 2
}

// jiraCategoryRank orders status categories as work flows: to do, in
// progress, done.
func jiraCategoryRank(c jira.Card) int {
	switch {
	case c.Done:
		return 2
	case c.InProgress:
		return 1
	}
	return 0
}

// jiraKeyNum is a key's number, for ABC-9 before ABC-10.
func jiraKeyNum(k string) int {
	n, _ := strconv.Atoi(k[strings.LastIndexByte(k, '-')+1:])
	return n
}

// apply sorts order (indexes into cards) in place, stably so ties keep rank.
func (s jiraSort) apply(order []int, cards []jira.Card) {
	if cmp := s.cmp(); cmp != nil {
		slices.SortStableFunc(order, func(a, b int) int { return cmp(cards[a], cards[b]) })
	}
}

// applyDesc is apply reversed; ties still keep rank.
func (s jiraSort) applyDesc(order []int, cards []jira.Card) {
	cmp := s.cmp()
	if cmp == nil {
		slices.Reverse(order)
		return
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp(cards[b], cards[a]) })
}

// viewSort is a list's order: by s, reversed when desc.
type viewSort struct {
	by   jiraSort
	desc bool
}

// next is the order a click on by's header gives: by, then by reversed,
// then rank again.
func (v viewSort) next(by jiraSort) viewSort {
	switch {
	case v.by != by:
		return viewSort{by: by}
	case !v.desc:
		return viewSort{by: by, desc: true}
	}
	return viewSort{}
}

// arrow marks the sorted column's header.
func (v viewSort) arrow() string {
	if v.desc {
		return "↓"
	}
	return "↑"
}

// cmp compares two cards by s; nil for rank, which keeps the given order.
func (s jiraSort) cmp() func(a, b jira.Card) int {
	var cmp func(a, b jira.Card) int
	switch s {
	case jiraSortPriority:
		cmp = func(a, b jira.Card) int { return jiraPriorityRank(a.Priority) - jiraPriorityRank(b.Priority) }
	case jiraSortPoints:
		pts := func(c jira.Card) float64 {
			if f, err := strconv.ParseFloat(c.Points, 64); err == nil {
				return f
			}
			return -1 // unestimated last
		}
		cmp = func(a, b jira.Card) int {
			pa, pb := pts(a), pts(b)
			switch {
			case pa > pb:
				return -1
			case pa < pb:
				return 1
			}
			return 0
		}
	case jiraSortAssignee:
		cmp = func(a, b jira.Card) int {
			switch {
			case a.Assignee == b.Assignee:
				return 0
			case a.Assignee == "":
				return 1 // unassigned last
			case b.Assignee == "":
				return -1
			}
			return strings.Compare(strings.ToLower(a.Assignee), strings.ToLower(b.Assignee))
		}
	case jiraSortEpic:
		cmp = func(a, b jira.Card) int {
			switch {
			case a.ParentSummary == b.ParentSummary:
				return 0
			case a.ParentSummary == "":
				return 1 // no epic last
			case b.ParentSummary == "":
				return -1
			}
			return strings.Compare(a.ParentSummary, b.ParentSummary)
		}
	case jiraSortStatus:
		cmp = func(a, b jira.Card) int {
			if c := jiraCategoryRank(a) - jiraCategoryRank(b); c != 0 {
				return c
			}
			return strings.Compare(a.Status, b.Status)
		}
	case jiraSortUpdated: // newest first
		cmp = func(a, b jira.Card) int { return b.Updated.Compare(a.Updated) }
	case jiraSortCreated: // newest first
		cmp = func(a, b jira.Card) int { return b.Created.Compare(a.Created) }
	case jiraSortDue: // soonest first, none last
		cmp = func(a, b jira.Card) int {
			switch {
			case a.Due.IsZero() && b.Due.IsZero():
				return 0
			case a.Due.IsZero():
				return 1
			case b.Due.IsZero():
				return -1
			}
			return a.Due.Compare(b.Due)
		}
	case jiraSortKey:
		cmp = func(a, b jira.Card) int {
			if c := strings.Compare(a.Key[:max(strings.LastIndexByte(a.Key, '-'), 0)], b.Key[:max(strings.LastIndexByte(b.Key, '-'), 0)]); c != 0 {
				return c
			}
			return jiraKeyNum(a.Key) - jiraKeyNum(b.Key)
		}
	}
	return cmp
}

// headCell is a sortable cell of the list's header: columns x0 to x1 of the
// body sort by by.
type headCell struct {
	x0, x1 int
	by     jiraSort
}

// jiraListHeader is the list's column header over rows laid out as
// jiraListRow lays them out with cols, and its sortable cells. A sort with
// no column of its own (updated, due, created) shows at the summary's end.
func (m *Model) jiraListHeader(cols listCols) (string, []headCell) {
	t := m.jiraTab
	f := m.opts.fields
	cur := viewSort{by: t.sort, desc: t.desc}
	var b strings.Builder
	var at []headCell
	x := 0
	// cell writes label in w columns, right-aligned when right, then gap
	// spaces; jiraSortRank for a column that doesn't sort.
	cell := func(label string, w, gap int, by jiraSort, right bool) {
		st := jiraDimStyle
		if by != jiraSortRank && cur.by == by {
			st = jiraViewActive
			if visualWidth(label)+1 > w {
				label = ansi.Truncate(label, max(w-1, 0), "")
			}
			label += cur.arrow()
		}
		label = ansi.Truncate(label, w, "…")
		pad := strings.Repeat(" ", max(w-visualWidth(label), 0))
		if right {
			b.WriteString(pad + st.Render(label))
		} else {
			b.WriteString(st.Render(label) + pad)
		}
		b.WriteString(strings.Repeat(" ", gap))
		if by != jiraSortRank {
			at = append(at, headCell{x0: x, x1: x + w, by: by})
		}
		x += w + gap
	}
	b.WriteString("  ")
	x += 2
	cell("Key", cols.key, 2, jiraSortKey, false)
	if f.typ {
		cell("", visualWidth(jiraTypeIcon("task", "")), 1, jiraSortRank, false)
	}
	if f.priority {
		cell("P", 1, 1, jiraSortPriority, false)
	}
	if f.status {
		cell("Status", cols.status, 2, jiraSortStatus, false)
	}
	if f.points {
		cell("Pts", 4, 2, jiraSortPoints, true)
	}
	tailW := 0
	if cols.who > 0 {
		tailW += 2 + cols.who
	}
	if cols.marks > 0 {
		tailW += 2 + cols.marks
	}
	titleW := max(cols.width-1-x-tailW, 0)
	end := x + titleW
	cell("Summary", min(len("Summary"), titleW), 0, jiraSortRank, false)
	if f.parent && x+3+len("Epic")+1 <= end {
		b.WriteString(jiraDimStyle.Render(" · "))
		x += 3
		cell("Epic", len("Epic")+1, 0, jiraSortEpic, false)
	}
	shown := map[jiraSort]bool{jiraSortRank: true, jiraSortKey: true, jiraSortPriority: f.priority, jiraSortStatus: f.status,
		jiraSortPoints: f.points, jiraSortEpic: f.parent, jiraSortAssignee: cols.who > 0}
	if by := cur.by.String(); !shown[cur.by] && x+len(by)+2 <= end {
		w := len(by) + 1
		b.WriteString(strings.Repeat(" ", end-w-x))
		x = end - w
		cell(strings.ToUpper(by[:1])+by[1:], w, 0, cur.by, true)
	}
	b.WriteString(strings.Repeat(" ", max(end-x, 0)))
	x = end
	if cols.who > 0 {
		b.WriteString("  ")
		x += 2
		cell("Assignee", cols.who, 0, jiraSortAssignee, false)
	}
	return ansi.Truncate(b.String(), max(cols.width-1, 0), ""), at
}
