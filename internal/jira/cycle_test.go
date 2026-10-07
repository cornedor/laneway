package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCycleTimes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/status":
			io.WriteString(w, `[{"id":"1","statusCategory":{"key":"new"}},{"id":"3","statusCategory":{"key":"indeterminate"}},{"id":"5","statusCategory":{"key":"done"}}]`)
		default:
			io.WriteString(w, `{"issues":[{"key":"A-1","fields":{"summary":"One","created":"2026-09-01T09:00:00.000+0000","resolutiondate":"2026-09-11T09:00:00.000+0000"},
				"changelog":{"histories":[{"created":"2026-09-08T09:00:00.000+0000","items":[{"field":"status","from":"1","to":"3"}]},
				{"created":"2026-09-11T09:00:00.000+0000","items":[{"field":"status","from":"3","to":"5"}]}]}},
				{"key":"A-2","fields":{"summary":"Two","created":"2026-09-01T09:00:00.000+0000","resolutiondate":"2026-09-02T09:00:00.000+0000"},"changelog":{"histories":[]}}]}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.CycleTimes(context.Background(), "A", 8, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "A-2" || got[1].Lead != 240*time.Hour || got[1].Cycle != 72*time.Hour || got[0].Cycle != 0 {
		t.Errorf("got %+v", got)
	}
	ds := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if Percentile(ds, 50) != 5 || Percentile(ds, 85) != 9 || Percentile(nil, 50) != 0 {
		t.Errorf("percentiles %v %v", Percentile(ds, 50), Percentile(ds, 85))
	}
}

// TestCycleTimesLines: done is when an issue got past the line; with a
// second one the cycle ends at the right line and Wait runs from the left.
func TestCycleTimesLines(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	at := func(daysAgo int) string { return now.AddDate(0, 0, -daysAgo).Format(jiraTime) }
	move := func(daysAgo int, from, to string) string {
		return `{"created":"` + at(daysAgo) + `","items":[{"field":"status","from":"` + from + `","to":"` + to + `"}]}`
	}
	var jql string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/status" {
			io.WriteString(w, `[{"id":"1","statusCategory":{"key":"new"}},{"id":"3","statusCategory":{"key":"indeterminate"}},{"id":"4","statusCategory":{"key":"indeterminate"}},{"id":"5","statusCategory":{"key":"done"}}]`)
			return
		}
		var body struct {
			JQL string `json:"jql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		jql = body.JQL
		io.WriteString(w, `{"issues":[
			{"key":"A-1","fields":{"summary":"Shipped","created":"`+at(10)+`","status":{"id":"5"}},"changelog":{"histories":[`+move(8, "1", "3")+`,`+move(5, "3", "4")+`,`+move(2, "4", "5")+`]}},
			{"key":"A-2","fields":{"summary":"In review","created":"`+at(10)+`","status":{"id":"4"}},"changelog":{"histories":[`+move(6, "1", "3")+`,`+move(3, "3", "4")+`]}},
			{"key":"A-3","fields":{"summary":"Old","created":"`+at(200)+`","status":{"id":"4"}},"changelog":{"histories":[`+move(100, "3", "4")+`]}}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cols := []Column{{Name: "To Do", StatusIDs: []string{"1"}}, {Name: "Doing", StatusIDs: []string{"3"}}, {Name: "Review", StatusIDs: []string{"4"}}, {Name: "Done", StatusIDs: []string{"5"}}}
	review, done := NewLine(cols, "Review", false), NewLine(cols, "Done", false)
	day := 24 * time.Hour
	got, err := c.CycleTimes(context.Background(), "A", 8, review, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jql, "status changed after -8w") {
		t.Errorf("jql = %q", jql)
	}
	if len(got) != 2 || got[0].Key != "A-1" || got[0].Cycle != 3*day || got[1].Key != "A-2" || got[1].Cycle != 3*day || got[0].Wait != 0 {
		t.Errorf("by review = %+v", got)
	}
	got, err = c.CycleTimes(context.Background(), "A", 8, review, done)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "A-1" || got[0].Cycle != 6*day || got[0].Wait != 3*day {
		t.Errorf("review to done = %+v", got)
	}
}
