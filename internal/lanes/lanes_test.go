package lanes

import (
	"reflect"
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
	got, hidden := Arrange(l, board)
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
	got, hidden := Arrange(l, board)
	want := []string{"In progress: In progress", "Later: Blocked UAT Done", "Test: Test"}
	if !reflect.DeepEqual(shape(got), want) {
		t.Fatalf("got %q, want %q", shape(got), want)
	}
	if !reflect.DeepEqual(hidden, []int{0}) {
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
	got, _ := Arrange(l, board)
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
		if got := Fits(c.l, 1, board); got != c.want {
			t.Errorf("%s fits: %v, want %v", c.l.Name, got, c.want)
		}
	}
	if !Fits(other, 9, board) {
		t.Error("d is for board 9")
	}
	if got := Fitting([]config.LaneLayout{two, one, {Lanes: two.Lanes}}, 1, board); len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Fitting: %v (a nameless layout is left out)", got)
	}
}

func TestBoard(t *testing.T) {
	got := Board(board)
	if len(got) != len(board) || got[3].Sections[0].Col != 3 || got[1].Max != 3 || got[3].Name != "Test" {
		t.Fatalf("Board: %+v", got)
	}
}
