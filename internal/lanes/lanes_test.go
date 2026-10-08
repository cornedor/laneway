package lanes

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

var board = []jira.Column{
	{Name: "To do", StatusIDs: []string{"1"}},
	{Name: "In progress", StatusIDs: []string{"3"}, Max: 3},
	{Name: "Blocked", StatusIDs: []string{"4"}, Max: 2},
	{Name: "Test", StatusIDs: []string{"10", "11"}},
	{Name: "UAT", StatusIDs: []string{"12"}},
	{Name: "Done", StatusIDs: []string{"20"}},
}

// shape is lanes as "name: sections" strings.
func shape(ls []Lane) []string {
	var out []string
	for _, l := range ls {
		s := l.Name + ":"
		for _, sec := range l.Sections {
			s += " " + sec.Name
		}
		out = append(out, s)
	}
	return out
}

func TestArrange(t *testing.T) {
	l := config.LaneLayout{Name: "dev", Lanes: []config.LaneSpec{
		{Statuses: []string{"1"}},
		{Name: "Doing", Statuses: []string{"3", "4"}},
		{Name: "Shipped", Statuses: []string{"11", "12", "20"}},
		{Name: "Gone", Statuses: []string{"99"}},
	}}
	got, hidden := Arrange(l, board, nil)
	want := []string{"To do: To do", "Doing: In progress Blocked", "Shipped: Test UAT Done"}
	if !reflect.DeepEqual(shape(got), want) || hidden != nil {
		t.Fatalf("got %q hidden %v, want %q", shape(got), hidden, want)
	}
	if got[1].Max != 5 || got[2].Max != 0 {
		t.Errorf("max %d, %d: want the sum when every section has one", got[1].Max, got[2].Max)
	}
	if ids := got[2].StatusIDs(); !reflect.DeepEqual(ids, []string{"10", "11", "12", "20"}) {
		t.Errorf("status ids %v", ids)
	}
	if got[0].Spec != 0 || got[2].Spec != 2 {
		t.Errorf("specs %d %d", got[0].Spec, got[2].Spec)
	}
	if got[2].Section("12") != 1 || got[2].Section("3") != -1 {
		t.Errorf("Section: %d %d", got[2].Section("12"), got[2].Section("3"))
	}
}

func TestArrangeOrderHiddenAndUnplaced(t *testing.T) {
	// Blocked stacks under the later Done lane; In progress isn't placed;
	// To do is hidden.
	l := config.LaneLayout{Name: "x", Hidden: []string{"1"}, Lanes: []config.LaneSpec{
		{Name: "Later", Statuses: []string{"12", "20", "4"}},
		{Statuses: []string{"10"}},
	}}
	got, hidden := Arrange(l, board, nil)
	want := []string{"In progress: In progress", "Later: Blocked UAT Done", "Test: Test"}
	if !reflect.DeepEqual(shape(got), want) {
		t.Fatalf("got %q, want %q", shape(got), want)
	}
	if !reflect.DeepEqual(hidden, []string{"1"}) {
		t.Errorf("hidden %v", hidden)
	}
	if got[0].Spec != -1 || got[1].Spec != 0 {
		t.Errorf("an unplaced column's lane has spec %d, Later %d", got[0].Spec, got[1].Spec)
	}
	if got[0].Sections[0].Col != 1 || got[1].Sections[0].Col != 2 {
		t.Errorf("cols %d %d", got[0].Sections[0].Col, got[1].Sections[0].Col)
	}
}

func TestArrangeUnplacedFollowsItsLeftNeighbour(t *testing.T) {
	l := config.LaneLayout{Name: "x", Lanes: []config.LaneSpec{
		{Statuses: []string{"20"}},
		{Statuses: []string{"3"}},
	}}
	got, _ := Arrange(l, board, nil)
	want := []string{"To do: To do", "Done: Done", "In progress: In progress", "Blocked: Blocked", "Test: Test", "UAT: UAT"}
	if !reflect.DeepEqual(shape(got), want) {
		t.Fatalf("got %q, want %q", shape(got), want)
	}
}

func TestFits(t *testing.T) {
	two := config.LaneLayout{Name: "a", Lanes: []config.LaneSpec{{Statuses: []string{"1", "3"}}}}
	one := config.LaneLayout{Name: "b", Lanes: []config.LaneSpec{{Statuses: []string{"20", "77"}}}}
	hid := config.LaneLayout{Name: "c", Lanes: []config.LaneSpec{{Statuses: []string{"20"}}}, Hidden: []string{"1"}}
	other := config.LaneLayout{Name: "d", Boards: []int{9}, Lanes: two.Lanes}
	for _, c := range []struct {
		l    config.LaneLayout
		want bool
	}{{two, true}, {one, false}, {hid, true}, {other, false}} {
		if got := Fits(c.l, "", 1, board); got != c.want {
			t.Errorf("%s fits: %v, want %v", c.l.Name, got, c.want)
		}
	}
	if !Fits(other, "", 9, board) {
		t.Error("d is for board 9")
	}
	if got := Fitting([]config.LaneLayout{two, one, {Lanes: two.Lanes}}, "", 1, board); len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Fitting: %v (a nameless layout is left out)", got)
	}
}

func TestBoard(t *testing.T) {
	got := Board(board)
	if len(got) != len(board) || got[3].Sections[0].Col != 3 || got[1].Max != 3 || got[3].Name != "Test" {
		t.Fatalf("Board: %+v", got)
	}
}

// TestDraft: a layout over a board keeps what other boards need: a lane
// with no column here, and statuses of columns elsewhere, through an edit.
func TestDraft(t *testing.T) {
	l := config.LaneLayout{Name: "x", Boards: []int{3}, Hidden: []string{"1", "77"}, Lanes: []config.LaneSpec{
		{Name: "Doing", Statuses: []string{"3", "88"}},
		{Name: "Elsewhere", Statuses: []string{"99"}},
		{Name: "Done", Statuses: []string{"20"}},
	}}
	d := NewDraft(l, board)
	var got []string
	for _, dl := range d.Lanes {
		got = append(got, strings.TrimSpace(fmt.Sprintf("%s %v %v", dl.Name, pieces(dl.Pieces), dl.Foreign)))
	}
	want := []string{"Doing [1] [88]", "Elsewhere [] [99]", "[2] []", "[3] []", "[4] []", "Done [5] []"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(d.Hidden, []Piece{{Col: 0}}) || !reflect.DeepEqual(d.ForeignHidden, []string{"77"}) {
		t.Fatalf("draft %q hidden %v %v, want %q", got, d.Hidden, d.ForeignHidden, want)
	}
	// Blocked joins Doing (after it, but stacked in board order), Test and
	// UAT go; Done is renamed to its column's name.
	d.Lanes[0].Pieces = []Piece{{Col: 2}, {Col: 1}}
	d.Lanes = append(d.Lanes[:2], d.Lanes[5])
	d.Lanes[2].Name = "Done"
	d.Hidden = append(d.Hidden, Piece{Col: 3}, Piece{Col: 4})
	out := d.Apply(l, board)
	if out.Name != "x" || !reflect.DeepEqual(out.Boards, []int{3}) {
		t.Errorf("name and boards: %+v", out)
	}
	wantLanes := []config.LaneSpec{{Name: "Doing", Statuses: []string{"3", "4", "88"}}, {Name: "Elsewhere", Statuses: []string{"99"}}, {Statuses: []string{"20"}}}
	if !reflect.DeepEqual(out.Lanes, wantLanes) || !reflect.DeepEqual(out.Hidden, []string{"1", "10", "11", "12", "77"}) {
		t.Errorf("applied %+v hidden %v", out.Lanes, out.Hidden)
	}
}

// pieces is ps as "3" for a whole column and "3:10" for a status of one.
func pieces(ps []Piece) []string {
	out := []string{}
	for _, p := range ps {
		s := fmt.Sprint(p.Col)
		if p.Status != "" {
			s += ":" + p.Status
		}
		out = append(out, s)
	}
	return out
}

// TestArrangeSplitColumn: Test's statuses over two lanes make a section in
// each, named by its statuses and without the column's limit; a status the
// layout doesn't list follows its column's first listed one.
func TestArrangeSplitColumn(t *testing.T) {
	cols := slices.Clone(board)
	cols[3] = jira.Column{Name: "Test", StatusIDs: []string{"10", "11", "13"}, Max: 4}
	names := map[string]string{"10": "Review", "11": "QA"}
	l := config.LaneLayout{Name: "x", Lanes: []config.LaneSpec{
		{Name: "Doing", Statuses: []string{"3", "10"}},
		{Name: "Check", Statuses: []string{"11", "12"}},
	}}
	got, _ := Arrange(l, cols, names)
	want := []string{"To do: To do", "Doing: In progress Review", "Blocked: Blocked", "Check: QA UAT", "Done: Done"}
	if !reflect.DeepEqual(shape(got), want) {
		t.Fatalf("got %q, want %q", shape(got), want)
	}
	if ids := got[1].StatusIDs(); !reflect.DeepEqual(ids, []string{"3", "10", "13"}) {
		t.Errorf("Doing has %v: 13 follows 10, its column's first listed", ids)
	}
	if got[1].Max != 0 || got[1].Sections[1].Col != 3 || got[3].Sections[0].Col != 3 {
		t.Errorf("max %d, cols %d %d", got[1].Max, got[1].Sections[1].Col, got[3].Sections[0].Col)
	}
	if !Fits(l, "", 1, cols) {
		t.Error("it places In progress, Test and UAT: fits")
	}
}

// TestDraftSplit: a split column's statuses are pieces of their own; moving
// one to another lane writes it there, and the draft made again keeps the
// split; taking the whole column gathers it.
func TestDraftSplit(t *testing.T) {
	l := config.LaneLayout{Name: "x", Lanes: []config.LaneSpec{{Statuses: []string{"3"}}, {Statuses: []string{"10", "11"}}}}
	d := NewDraft(l, board)
	if got := pieces(d.Lanes[3].Pieces); !reflect.DeepEqual(got, []string{"3"}) {
		t.Fatalf("Test lane %v", got)
	}
	d.Split(3, board)
	if got := pieces(d.Lanes[3].Pieces); !reflect.DeepEqual(got, []string{"3:10", "3:11"}) {
		t.Fatalf("split %v", got)
	}
	p := Piece{Col: 3, Status: "11"}
	if d.LaneOf(p) != 3 {
		t.Errorf("LaneOf %d", d.LaneOf(p))
	}
	d.Take(p)
	d.Lanes[1].Pieces = append(d.Lanes[1].Pieces, p)
	out := d.Apply(l, board)
	want := []config.LaneSpec{{Statuses: []string{"1"}}, {Statuses: []string{"3", "11"}}, {Statuses: []string{"4"}}, {Statuses: []string{"10"}}, {Statuses: []string{"12"}}, {Statuses: []string{"20"}}}
	if !reflect.DeepEqual(out.Lanes, want) {
		t.Fatalf("applied %+v", out.Lanes)
	}
	d = NewDraft(out, board)
	if a, b := pieces(d.Lanes[1].Pieces), pieces(d.Lanes[3].Pieces); !reflect.DeepEqual(a, []string{"1", "3:11"}) || !reflect.DeepEqual(b, []string{"3:10"}) {
		t.Fatalf("made again: %v %v", a, b)
	}
	d.Take(Piece{Col: 3})
	d.Lanes[3].Pieces = append(d.Lanes[3].Pieces, Piece{Col: 3})
	if got := d.Apply(out, board).Lanes; !reflect.DeepEqual(got[1].Statuses, []string{"3"}) || !reflect.DeepEqual(got[3].Statuses, []string{"10", "11"}) {
		t.Errorf("gathered %+v", got)
	}
}

// TestArrangeSplitLaneName: a status moved onto another column's lane
// leaves the lane that column's name, though it comes first.
func TestArrangeSplitLaneName(t *testing.T) {
	l := config.LaneLayout{Name: "x", Lanes: []config.LaneSpec{{Statuses: []string{"10"}}, {Statuses: []string{"11", "20"}}}}
	got, _ := Arrange(l, board, map[string]string{"11": "QA"})
	if names := shape(got); !slices.Contains(names, "Done: QA Done") {
		t.Errorf("lanes %q, want Done's lane named Done", names)
	}
	d := NewDraft(l, board)
	li := d.LaneOf(Piece{Col: 5})
	d.Lanes[li].Name = "Done"
	if out := d.Apply(l, board); out.Lanes[li].Name != "" || len(out.Lanes[li].Statuses) != 2 {
		t.Errorf("Done named as its column: %+v, want the name left out", out.Lanes[li])
	}
	d.Lanes[li].Name = "QA"
	if out := d.Apply(l, board); out.Lanes[li].Name != "QA" {
		t.Errorf("named as its status: %+v, want the name kept", out.Lanes[li])
	}
}

// TestFitsSite: a layout made on one Jira stays off another's boards, whose
// status ids mean other statuses; one made on none fits every site.
func TestFitsSite(t *testing.T) {
	l := config.LaneLayout{Name: "dev", Site: "work.atlassian.net", Lanes: []config.LaneSpec{{Statuses: []string{"1"}}, {Statuses: []string{"3"}}}}
	if !Fits(l, Site("https://work.atlassian.net/"), 1, board) || !Fits(l, "WORK.atlassian.net", 1, board) {
		t.Error("made here: fits")
	}
	if Fits(l, Site("http://127.0.0.1:8080"), 1, board) {
		t.Error("made on another site: fits")
	}
	l.Site = ""
	if !Fits(l, "127.0.0.1:8080", 1, board) {
		t.Error("made on no site in particular: doesn't fit")
	}
}
