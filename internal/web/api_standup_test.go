package web

import (
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/standup"
	"github.com/cornedor/laneway/internal/work"
)

func TestStandupLinesAndProposals(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	since := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)
	var out struct {
		Stops    []standup.Stop
		Board    []standup.Column
		Since    string
		Settings struct{ Length, Timebox float64 }
	}
	if code := issueCall(t, "GET", ts.URL+"/api/standup/lines?board=1&sprint=12", nil, &out); code != 200 {
		t.Fatalf("lines: %d", code)
	}
	if len(out.Stops) < 2 || out.Stops[0].Person.ID != "" || out.Stops[1].Person.Name == "" || out.Since == "" ||
		out.Settings.Length != 900 || out.Settings.Timebox != 0 {
		t.Errorf("stops %+v since %q settings %+v", out.Stops, out.Since, out.Settings)
	}
	// The board beside it: the view's columns in order, each card in its column.
	n := 0
	for _, col := range out.Board {
		n += len(col.Cards)
	}
	if len(out.Board) < 2 || out.Board[0].Name == "" || n == 0 {
		t.Errorf("board: %+v", out.Board)
	}
	var pr struct{ Items []work.Proposal }
	if code := issueCall(t, "GET", ts.URL+"/api/worklog/proposals?day="+time.Now().Format(time.DateOnly), nil, &pr); code != 200 {
		t.Errorf("proposals: %d", code)
	}
	if code := issueCall(t, "GET", ts.URL+"/api/standup/lines?since="+since, nil, nil); code != 400 {
		t.Errorf("without board: %d", code)
	}
	// The team walk takes the board's view: a sprint, the backlog, a query.
	keys := func(q string) []string {
		var r struct{ Stops []standup.Stop }
		if code := issueCall(t, "GET", ts.URL+"/api/standup/lines?since="+since+"&board=1&"+q, nil, &r); code != 200 {
			t.Fatalf("%s: %d", q, code)
		}
		var ks []string
		for _, l := range append(r.Stops[0].Rows, r.Stops[0].Folded...) {
			if l.Key != "" {
				ks = append(ks, l.Key)
			}
		}
		return ks
	}
	sprint, backlog, query := keys("sprint=12"), keys("backlog=1"), keys("kind=filter&jql="+url.QueryEscape("key = DEMO-4"))
	if len(sprint) == 0 || slices.ContainsFunc(backlog, func(k string) bool { return slices.Contains(sprint, k) }) {
		t.Errorf("sprint %v, backlog %v", sprint, backlog)
	}
	if !slices.Contains(query, "DEMO-4") || slices.Contains(query, "DEMO-8") { // Jamie's work, not Mira's
		t.Errorf("query view = %v", query)
	}
}
