package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/lanes"
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
	got := boardLayouts(ls, 1, cols, nil)
	if len(got) != 1 || got[0].Name != "Ship" {
		t.Fatalf("layouts %+v, want Ship alone", got)
	}
	b, _ := json.Marshal(got[0])
	for _, want := range []string{`"Lanes":[{"Name":"Done","StatusIDs":["2","3"],"Max":0,"Sections":[{"Name":"Test"`, `"Hidden":["1"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s", b, want)
		}
	}
	if got := boardLayouts(nil, 1, cols, nil); got == nil {
		t.Error("no layouts should be [], not null")
	}
}

// TestArrangeBoard: the lane editor's arrangement of a layout over the demo
// board: its columns as pieces, the layout lane each came from.
func TestArrangeBoard(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, Demo: true}))
	defer ts.Close()
	body := `{"Layout": {"name": "Ship", "lanes": [{"name": "Shipping", "statuses": ["3", "4"]}], "hidden": ["1"]}}`
	resp, err := http.Post(ts.URL+"/api/boards/1/arrange", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Columns []jira.Column
		Layout  config.LaneLayout
		Draft   struct {
			Lanes  []lanes.DraftLane
			Hidden []lanes.Piece
		}
		Fits bool
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != 200 {
		t.Fatalf("%d %v", resp.StatusCode, err)
	}
	if len(got.Columns) != 4 || !got.Fits || len(got.Draft.Hidden) != 1 || got.Draft.Hidden[0] != (lanes.Piece{Col: 0}) || got.Layout.Name != "Ship" {
		t.Fatalf("columns %d, fits %v, draft %+v, layout %+v", len(got.Columns), got.Fits, got.Draft, got.Layout)
	}
	if ls := got.Draft.Lanes; len(ls) != 2 || ls[0].Name != "" || ls[1].Name != "Shipping" || len(ls[1].Pieces) != 2 {
		t.Errorf("lanes %+v, want In Progress unplaced then Shipping of columns 2 and 3", ls)
	}
	// A draft comes back applied: In Progress joins Shipping.
	body = `{"Layout": {"name": "Ship"}, "Draft": {"Lanes": [{"Name": "Shipping", "Pieces": [{"Col": 1}, {"Col": 2}, {"Col": 3}]}], "Hidden": [{"Col": 0}]}}`
	resp2, err := http.Post(ts.URL+"/api/boards/1/arrange", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if err := json.NewDecoder(resp2.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if ls := got.Layout.Lanes; len(ls) != 1 || strings.Join(ls[0].Statuses, ",") != "2,3,5,4" || strings.Join(got.Layout.Hidden, ",") != "1" {
		t.Errorf("applied layout %+v", got.Layout)
	}
}
