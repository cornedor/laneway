// Package lanes arranges a board's columns into the lanes the terminal and
// the browser draw: the board's own, one column each, or a ui.lane_layouts
// entry's, which stacks columns in a lane, splits a column's statuses over
// lanes, reorders, renames and hides them.
package lanes

import (
	"net/url"
	"slices"
	"strings"

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
	// Spec is the index of the layout's lane it is, -1 for a column the
	// layout doesn't place (and the board's own lanes).
	Spec int
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
		out[i] = Lane{Name: c.Name, Max: c.Max, Spec: -1, Sections: []Section{{Name: c.Name, StatusIDs: c.StatusIDs, Max: c.Max, Col: i}}}
	}
	return out
}

// hiddenLane is where placement puts a status the layout hides.
const hiddenLane = -2

// placement is where l puts each status of each of cols, by column then
// status: the index of the lane listing it, hiddenLane when Hidden lists
// it, -1 for nowhere. A status l doesn't list goes where its column's first
// listed one goes, so a status added to a column later follows it; a
// column l lists none of goes nowhere.
func placement(l config.LaneLayout, cols []jira.Column) [][]int {
	listed := func(id string) int {
		if i := slices.IndexFunc(l.Lanes, func(s config.LaneSpec) bool { return slices.Contains(s.Statuses, id) }); i >= 0 {
			return i
		}
		if slices.Contains(l.Hidden, id) {
			return hiddenLane
		}
		return -1
	}
	out := make([][]int, len(cols))
	for ci, c := range cols {
		at, home := make([]int, len(c.StatusIDs)), -1
		for si, id := range c.StatusIDs {
			if at[si] = listed(id); home == -1 {
				home = at[si]
			}
		}
		for si := range at {
			if at[si] == -1 {
				at[si] = home
			}
		}
		out[ci] = at
	}
	return out
}

// placed is whether at, a column's placement, puts it anywhere.
func placed(at []int) bool { return len(at) > 0 && at[0] != -1 }

// Site is the host of baseURL, as a layout's Site names a Jira.
func Site(baseURL string) string {
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return baseURL
}

// Fits is whether l is for board of site (a Site): made there, or on no
// site in particular; listed in its Boards, or Boards empty; and placing at
// least two of cols (Jira's default statuses are in many workflows; one
// shared column alone doesn't make a board's layout).
func Fits(l config.LaneLayout, site string, board int, cols []jira.Column) bool {
	if l.Site != "" && site != "" && !strings.EqualFold(l.Site, site) {
		return false
	}
	if len(l.Boards) > 0 && !slices.Contains(l.Boards, board) {
		return false
	}
	n := 0
	for _, at := range placement(l, cols) {
		if placed(at) {
			n++
		}
	}
	return n >= 2
}

// Fitting are the layouts for board of site, in config order.
func Fitting(ls []config.LaneLayout, site string, board int, cols []jira.Column) []config.LaneLayout {
	var out []config.LaneLayout
	for _, l := range ls {
		if l.Name != "" && Fits(l, site, board, cols) {
			out = append(out, l)
		}
	}
	return out
}

// Arrange is cols in l's lanes, and the statuses it hides. A column whose
// statuses l spreads over lanes is a section in each, named by its statuses
// there (names, falling back on the column's) and without the column's
// limit. A lane l doesn't name is its first whole column's, else its first
// section's. A lane of l without a status here is left out; a column l doesn't
// place gets a lane of its own after the lane of the column left of it
// (first when none is), so a column added to the board later still shows.
func Arrange(l config.LaneLayout, cols []jira.Column, names map[string]string) (out []Lane, hidden []string) {
	at := placement(l, cols)
	for ci, c := range cols {
		for si, a := range at[ci] {
			if a == hiddenLane {
				hidden = append(hidden, c.StatusIDs[si])
			}
		}
	}
	for li, s := range l.Lanes {
		lane := Lane{Spec: li}
		for ci, c := range cols {
			var ids []string
			for si, a := range at[ci] {
				if a == li {
					ids = append(ids, c.StatusIDs[si])
				}
			}
			if len(ids) == 0 {
				continue
			}
			sec := Section{Name: c.Name, StatusIDs: ids, Max: c.Max, Col: ci}
			if len(ids) < len(c.StatusIDs) {
				sec.Name, sec.Max = statusesName(ids, names, c.Name), 0
			}
			lane.Sections = append(lane.Sections, sec)
		}
		if len(lane.Sections) == 0 {
			continue
		}
		lane.Name = s.Name
		if lane.Name == "" {
			lane.Name = lane.Sections[0].Name
			if i := slices.IndexFunc(lane.Sections, func(sec Section) bool { return len(sec.StatusIDs) == len(cols[sec.Col].StatusIDs) }); i >= 0 {
				lane.Name = lane.Sections[i].Name
			}
		}
		lane.Max = sumMax(lane.Sections)
		out = append(out, lane)
	}
	for ci := range cols {
		if placed(at[ci]) {
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

// statusesName is ids by name, "" for one names lacks; fallback when none
// has one.
func statusesName(ids []string, names map[string]string, fallback string) string {
	var ns []string
	for _, id := range ids {
		if n := names[id]; n != "" {
			ns = append(ns, n)
		}
	}
	if len(ns) == 0 {
		return fallback
	}
	return strings.Join(ns, ", ")
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

// Draft is a layout as an editor shows it over one board: lanes of pieces,
// each keeping the statuses of its columns on other boards (Foreign), and
// the hidden pieces. Lane names are "" for the first column's.
type Draft struct {
	Lanes         []DraftLane
	Hidden        []Piece
	ForeignHidden []string
}

// DraftLane is a lane of a Draft.
type DraftLane struct {
	Name    string
	Pieces  []Piece
	Foreign []string
}

// Piece is what an editor moves: a board column whole, or one status of a
// column split over lanes.
type Piece struct {
	Col    int
	Status string `json:",omitempty"` // "" for the whole column
}

// NewDraft is l over cols. A column all of whose statuses sit in one place
// is one piece, else a piece per status. A lane of l without a column here
// stays, after the one before it, for the boards it is for.
func NewDraft(l config.LaneLayout, cols []jira.Column) Draft {
	here := map[string]bool{}
	for _, c := range cols {
		for _, id := range c.StatusIDs {
			here[id] = true
		}
	}
	foreign := func(ids []string) []string {
		out := []string{}
		for _, id := range ids {
			if !here[id] {
				out = append(out, id)
			}
		}
		return out
	}
	pieces := func(ci int, ids []string) []Piece {
		if len(ids) == len(cols[ci].StatusIDs) {
			return []Piece{{Col: ci}}
		}
		out := make([]Piece, len(ids))
		for i, id := range ids {
			out[i] = Piece{Col: ci, Status: id}
		}
		return out
	}
	arranged, hidden := Arrange(l, cols, nil)
	d := Draft{Hidden: []Piece{}, ForeignHidden: foreign(l.Hidden)}
	for ci, c := range cols {
		if ids := slices.DeleteFunc(slices.Clone(c.StatusIDs), func(id string) bool { return !slices.Contains(hidden, id) }); len(ids) > 0 {
			d.Hidden = append(d.Hidden, pieces(ci, ids)...)
		}
	}
	spec := []int{} // each draft lane's layout lane, -1 for none
	for _, a := range arranged {
		dl := DraftLane{Pieces: []Piece{}, Foreign: []string{}}
		for _, s := range a.Sections {
			dl.Pieces = append(dl.Pieces, pieces(s.Col, s.StatusIDs)...)
		}
		if a.Spec >= 0 {
			dl.Name, dl.Foreign = l.Lanes[a.Spec].Name, foreign(l.Lanes[a.Spec].Statuses)
		}
		d.Lanes, spec = append(d.Lanes, dl), append(spec, a.Spec)
	}
	for si, s := range l.Lanes {
		if slices.Contains(spec, si) {
			continue
		}
		at := 0
		for j, sj := range spec {
			if sj >= 0 && sj < si {
				at = j + 1
			}
		}
		d.Lanes = slices.Insert(d.Lanes, at, DraftLane{Name: s.Name, Pieces: []Piece{}, Foreign: foreign(s.Statuses)})
		spec = slices.Insert(spec, at, si)
	}
	return d
}

// Split shows column ci status by status: its whole piece, wherever it is,
// becomes a piece per status in its place.
func (d *Draft) Split(ci int, cols []jira.Column) {
	split := func(ps []Piece) []Piece {
		i := slices.Index(ps, Piece{Col: ci})
		if i < 0 || ci >= len(cols) {
			return ps
		}
		var each []Piece
		for _, id := range cols[ci].StatusIDs {
			each = append(each, Piece{Col: ci, Status: id})
		}
		return slices.Concat(ps[:i], each, ps[i+1:])
	}
	for i := range d.Lanes {
		d.Lanes[i].Pieces = split(d.Lanes[i].Pieces)
	}
	d.Hidden = split(d.Hidden)
}

// Take lifts p out of its lane or the hidden ones, and a whole column's
// statuses with it; an emptied lane stays for Apply to drop, so lane
// indexes hold.
func (d *Draft) Take(p Piece) {
	gone := func(q Piece) bool { return q == p || p.Status == "" && q.Col == p.Col }
	for i := range d.Lanes {
		d.Lanes[i].Pieces = slices.DeleteFunc(d.Lanes[i].Pieces, gone)
	}
	d.Hidden = slices.DeleteFunc(d.Hidden, gone)
}

// LaneOf is the index of the lane holding p, -1 for none (hidden).
func (d Draft) LaneOf(p Piece) int {
	return slices.IndexFunc(d.Lanes, func(l DraftLane) bool { return slices.Contains(l.Pieces, p) })
}

// Apply is l with d's lanes and hidden pieces, cols being the board's d
// was made over. A lane left without a status goes; a name that is its
// first whole column's is left out.
func (d Draft) Apply(l config.LaneLayout, cols []jira.Column) config.LaneLayout {
	byCol := func(a, b Piece) int { return a.Col - b.Col }
	ids := func(ps []Piece) []string {
		ps = slices.Clone(ps)
		slices.SortStableFunc(ps, byCol)
		var out []string
		for _, p := range ps {
			switch {
			case p.Col < 0 || p.Col >= len(cols):
			case p.Status == "":
				out = append(out, cols[p.Col].StatusIDs...)
			case slices.Contains(cols[p.Col].StatusIDs, p.Status):
				out = append(out, p.Status)
			}
		}
		return out
	}
	l.Lanes = nil
	for _, dl := range d.Lanes {
		st := append(ids(dl.Pieces), dl.Foreign...)
		if len(st) == 0 {
			continue
		}
		name := dl.Name
		whole := slices.DeleteFunc(slices.Clone(dl.Pieces), func(p Piece) bool { return p.Status != "" || p.Col < 0 || p.Col >= len(cols) })
		if len(whole) > 0 && name == cols[slices.MinFunc(whole, byCol).Col].Name {
			name = ""
		}
		l.Lanes = append(l.Lanes, config.LaneSpec{Name: name, Statuses: st})
	}
	l.Hidden = append(ids(d.Hidden), d.ForeignHidden...)
	return l
}
