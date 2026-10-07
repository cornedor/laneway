package jira

import (
	"math"
	"slices"
	"strings"
	"time"
)

// A line across a board: an issue is past it while its status sits in the
// line's column or one to its right. The reports count done by one, by
// default Jira's (the resolution date), and can draw a second beside it.

// Line is a board column as a point issues pass. A nil *Line is Jira's
// done: the resolution date.
type Line struct {
	Name string
	// First counts an issue past the line from its first crossing on, also
	// after it moved back; else only while it stands there.
	First    bool
	at       int
	statuses map[string]bool
}

// NewLine is the line at the column of cols named name; nil for "" or a
// name no column has.
func NewLine(cols []Column, name string, first bool) *Line {
	if name == "" {
		return nil
	}
	i := slices.IndexFunc(cols, func(c Column) bool { return strings.EqualFold(c.Name, name) })
	if i < 0 {
		return nil
	}
	l := &Line{Name: cols[i].Name, First: first, at: i, statuses: map[string]bool{}}
	for _, c := range cols[i:] {
		for _, id := range c.StatusIDs {
			l.statuses[id] = true
		}
	}
	return l
}

// Label is the line's column, "done" for Jira's.
func (l *Line) Label() string {
	if l == nil {
		return "done"
	}
	return l.Name
}

// Past is whether is stood past the line at t. Its Moves must be read
// (SprintBurn) for a column's line.
func (l *Line) Past(is BurnIssue, t time.Time) bool {
	switch {
	case l == nil:
		return !is.Resolved.IsZero() && !is.Resolved.After(t)
	case l.First:
		at, ok := l.Since(is)
		return ok && !at.After(t)
	}
	return l.statuses[is.StatusAt(t)]
}

// Since is when is got past the line as it counts now: its resolution for
// Jira's done; its first crossing with First; else the start of its stay
// there, false while it stands before the line. A zero time with true: it
// was past before its history starts.
func (l *Line) Since(is BurnIssue) (time.Time, bool) {
	if l == nil {
		return is.Resolved, !is.Resolved.IsZero()
	}
	if l.First {
		if l.statuses[is.StatusAt(time.Time{})] { // where its history starts
			return time.Time{}, true
		}
		for _, mv := range is.Moves {
			if l.statuses[mv.To] {
				return mv.When, true
			}
		}
		return time.Time{}, false
	}
	if !l.statuses[is.Status] {
		return time.Time{}, false
	}
	for i := len(is.Moves) - 1; i >= 0; i-- {
		if !l.statuses[is.Moves[i].From] {
			return is.Moves[i].When, true
		}
	}
	return time.Time{}, true
}

// Order is a and b, the one further left first; Jira's done comes last.
func Order(a, b *Line) (*Line, *Line) {
	if a.pos() > b.pos() {
		return b, a
	}
	return a, b
}

func (l *Line) pos() int {
	if l == nil {
		return math.MaxInt
	}
	return l.at
}
