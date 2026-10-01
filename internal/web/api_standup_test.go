package web

import (
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

func TestTeamWalk(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	cols := []teamColumn{
		{"To Do", []jira.Card{{Key: "AB-1", Summary: "a"}}},
		{"Doing", []jira.Card{{Key: "AB-2", Summary: "b", InProgress: true, Assignee: "Ann", Since: now.AddDate(0, 0, -9)}}},
	}
	entries := []jira.InboxEntry{{Key: "AB-9", Summary: "off", Who: "Bob", What: "commit: x", When: now}}
	items, folded, text := teamWalk(cols, entries, []string{"AB"}, false, 5, now.AddDate(0, 0, -1), now, nil)
	if len(items) != 3 || items[0].Head != "Doing (1)" || items[1].Age != "9d stale" || items[1].What != "no activity" {
		t.Fatalf("items: %+v", items)
	}
	if items[2].Unfold != true || len(folded) != 1 || folded[0].Key != "AB-9" || folded[0].Who != "Bob" {
		t.Errorf("off the board: %+v %+v", items[2], folded)
	}
	if text == "" {
		t.Error("no text")
	}
	byP, _, _ := teamWalk(cols, entries, nil, true, 5, now, now, nil)
	if byP[0].Head != "Ann (1)" {
		t.Errorf("by person: %+v", byP)
	}
}

func TestStandupLinesAndProposals(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	since := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)
	var out struct{ Lines []StandupRow }
	if code := issueCall(t, "GET", ts.URL+"/api/standup/lines?since="+since, nil, &out); code != 200 {
		t.Fatalf("lines: %d", code)
	}
	var pr struct{ Items []work.Proposal }
	if code := issueCall(t, "GET", ts.URL+"/api/worklog/proposals?day="+time.Now().Format(time.DateOnly), nil, &pr); code != 200 {
		t.Errorf("proposals: %d", code)
	}
	if code := issueCall(t, "GET", ts.URL+"/api/standup/lines?since="+since+"&mode=team", nil, nil); code != 400 {
		t.Errorf("team without board: %d", code)
	}
	// The team walk takes the board's view: a sprint, the backlog, a query.
	keys := func(q string) []string {
		var r struct{ Lines, Folded []StandupRow }
		if code := issueCall(t, "GET", ts.URL+"/api/standup/lines?since="+since+"&mode=team&board=1&"+q, nil, &r); code != 200 {
			t.Fatalf("%s: %d", q, code)
		}
		var ks []string
		for _, l := range append(r.Lines, r.Folded...) {
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
