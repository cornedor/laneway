package ui

import (
	"slices"
	"strconv"
	"strings"

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
	jiraSortCount
)

func (s jiraSort) String() string {
	return [...]string{"rank", "priority", "points", "assignee", "epic", "key", "status"}[s]
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
