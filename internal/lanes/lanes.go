// Package lanes arranges a board's columns into the lanes the terminal and
// the browser draw: the board's own, one column each, or a ui.lane_layouts
// entry's, which stacks columns in a lane, reorders, renames and hides them.
package lanes

import (
	"slices"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// Section is a board column in a lane; Col is its index in the columns
// the lanes were made of.
type Section struct {
	Name      string
	StatusIDs []string
	Max       int
	Col       int
}

// Lane is a lane on the board: one section, or several stacked in board
// order.
type Lane struct {
	Name     string
	Sections []Section
	// Max is the work-in-progress limit: its sections' summed when every
	// one has a limit, else 0 for none.
	Max int
}

// StatusIDs are the lane's statuses, its sections' in order.
func (l Lane) StatusIDs() []string {
	var out []string
	for _, s := range l.Sections {
		out = append(out, s.StatusIDs...)
	}
	return out
}

// Section is the index of the section holding status id, -1 for none.
func (l Lane) Section(id string) int {
	return slices.IndexFunc(l.Sections, func(s Section) bool { return slices.Contains(s.StatusIDs, id) })
}

// Board is cols as the board has them: a lane each.
func Board(cols []jira.Column) []Lane {
	out := make([]Lane, len(cols))
	for i, c := range cols {
		out[i] = Lane{Name: c.Name, Max: c.Max, Sections: []Section{{Name: c.Name, StatusIDs: c.StatusIDs, Max: c.Max, Col: i}}}
	}
	return out
}

// place is where l puts column c: the index of the lane listing one of
// its statuses, hidden when Hidden lists one, else -1 for nowhere.
func place(l config.LaneLayout, c jira.Column) (lane int, hidden bool) {
	has := func(ids []string) bool {
		return slices.ContainsFunc(c.StatusIDs, func(id string) bool { return slices.Contains(ids, id) })
	}
	for i, s := range l.Lanes {
		if has(s.Statuses) {
			return i, false
		}
	}
	return -1, has(l.Hidden)
}

// Fits is whether l is for board: listed in its Boards, or Boards empty,
// and placing at least two of cols (Jira's default statuses are in many
// workflows; one shared column alone doesn't make a board's layout).
func Fits(l config.LaneLayout, board int, cols []jira.Column) bool {
	if len(l.Boards) > 0 && !slices.Contains(l.Boards, board) {
		return false
	}
	n := 0
	for _, c := range cols {
		if i, hidden := place(l, c); i >= 0 || hidden {
			n++
		}
	}
	return n >= 2
}

// Fitting are the layouts for board, in config order.
func Fitting(ls []config.LaneLayout, board int, cols []jira.Column) []config.LaneLayout {
	var out []config.LaneLayout
	for _, l := range ls {
		if l.Name != "" && Fits(l, board, cols) {
			out = append(out, l)
		}
	}
	return out
}

// Arrange is cols in l's lanes, and the indexes of the columns it hides. A
// lane of l without a column here is left out; a column l doesn't place
// gets a lane of its own after the lane of the column left of it (first
// when none is), so a column added to the board later still shows.
func Arrange(l config.LaneLayout, cols []jira.Column) (out []Lane, hidden []int) {
	at := make([]int, len(cols)) // the column's lane in l, -1 for none
	for ci, c := range cols {
		i, h := place(l, c)
		at[ci] = i
		if h {
			hidden = append(hidden, ci)
		}
	}
	for li, s := range l.Lanes {
		var lane Lane
		for ci, c := range cols {
			if at[ci] == li {
				lane.Sections = append(lane.Sections, Section{Name: c.Name, StatusIDs: c.StatusIDs, Max: c.Max, Col: ci})
			}
		}
		if len(lane.Sections) == 0 {
			continue
		}
		lane.Name = s.Name
		if lane.Name == "" {
			lane.Name = lane.Sections[0].Name
		}
		lane.Max = sumMax(lane.Sections)
		out = append(out, lane)
	}
	for ci := range cols {
		if at[ci] >= 0 || slices.Contains(hidden, ci) {
			continue
		}
		// After the lane of the nearest column to the left still shown.
		pos := 0
		for left := ci - 1; left >= 0; left-- {
			if li := laneOf(out, left); li >= 0 {
				pos = li + 1
				break
			}
		}
		out = slices.Insert(out, pos, Board(cols[ci : ci+1])[0])
		out[pos].Sections[0].Col = ci
	}
	return out, hidden
}

// laneOf is the index of the lane holding column ci, -1 for none.
func laneOf(ls []Lane, ci int) int {
	return slices.IndexFunc(ls, func(l Lane) bool {
		return slices.ContainsFunc(l.Sections, func(s Section) bool { return s.Col == ci })
	})
}

func sumMax(ss []Section) int {
	n := 0
	for _, s := range ss {
		if s.Max <= 0 {
			return 0
		}
		n += s.Max
	}
	return n
}
