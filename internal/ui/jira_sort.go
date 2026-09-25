package ui

import (
	"slices"
	"strconv"
	"strings"

	"jiratui/internal/jira"
)

// jiraSort orders the list view; s cycles it.
type jiraSort int

const (
	jiraSortRank jiraSort = iota
	jiraSortPriority
	jiraSortPoints
	jiraSortAssignee
	jiraSortKey
	jiraSortCount
)

func (s jiraSort) String() string {
	return [...]string{"rank", "priority", "points", "assignee", "key"}[s]
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

// jiraKeyNum is a key's number, for ABC-9 before ABC-10.
func jiraKeyNum(k string) int {
	n, _ := strconv.Atoi(k[strings.LastIndexByte(k, '-')+1:])
	return n
}

// apply sorts order (indexes into cards) in place, stably so ties keep rank.
func (s jiraSort) apply(order []int, cards []jira.Card) {
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
	case jiraSortKey:
		cmp = func(a, b jira.Card) int {
			if c := strings.Compare(a.Key[:max(strings.LastIndexByte(a.Key, '-'), 0)], b.Key[:max(strings.LastIndexByte(b.Key, '-'), 0)]); c != 0 {
				return c
			}
			return jiraKeyNum(a.Key) - jiraKeyNum(b.Key)
		}
	default:
		return
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp(cards[a], cards[b]) })
}
