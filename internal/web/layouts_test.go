package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// TestBoardLayouts: the bundle's layouts are the fitting ones, their lanes
// shaped as columns with the stacked ones as sections.
func TestBoardLayouts(t *testing.T) {
	cols := []jira.Column{
		{Name: "To do", StatusIDs: []string{"1"}},
		{Name: "Test", StatusIDs: []string{"2"}},
		{Name: "Done", StatusIDs: []string{"3"}, Max: 4},
	}
	ls := []config.LaneLayout{
		{Name: "Ship", Hidden: []string{"1"}, Lanes: []config.LaneSpec{{Name: "Done", Statuses: []string{"2", "3"}}}},
		{Name: "Other board", Boards: []int{5}, Lanes: []config.LaneSpec{{Statuses: []string{"1", "2"}}}},
	}
	got := boardLayouts(ls, 1, cols)
	if len(got) != 1 || got[0].Name != "Ship" {
		t.Fatalf("layouts %+v, want Ship alone", got)
	}
	b, _ := json.Marshal(got[0])
	for _, want := range []string{`"Lanes":[{"Name":"Done","StatusIDs":["2","3"],"Max":0,"Sections":[{"Name":"Test"`, `"Hidden":["1"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s", b, want)
		}
	}
	if got := boardLayouts(nil, 1, cols); got == nil {
		t.Error("no layouts should be [], not null")
	}
}
