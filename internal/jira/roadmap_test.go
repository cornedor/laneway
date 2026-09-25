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

// TestRoadmap: epics with their own dates, dates from the children's
// sprints, and progress by children and points.
func TestRoadmap(t *testing.T) {
	var jqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/field":
			io.WriteString(w, `[{"id":"customfield_10","name":"Story Points"},
			  {"id":"customfield_20","name":"Start date"},
			  {"id":"customfield_30","name":"Sprint","schema":{"custom":"com.pyxis.greenhopper.jira:gh-sprint"}}]`)
		case "/rest/api/3/search/jql":
			var body struct {
				JQL string `json:"jql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			jqls = append(jqls, body.JQL)
			if strings.HasPrefix(body.JQL, "project") {
				io.WriteString(w, `{"issues":[
				  {"key":"ABC-1","fields":{"summary":"Checkout","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},
				   "customfield_20":"2026-09-01","duedate":"2026-10-15"}},
				  {"key":"ABC-2","fields":{"summary":"Search","status":{"name":"To Do","statusCategory":{"key":"new"}}}}]}`)
				return
			}
			io.WriteString(w, `{"issues":[
			  {"key":"ABC-3","fields":{"parent":{"key":"ABC-1"},"status":{"statusCategory":{"key":"done"}},"customfield_10":3}},
			  {"key":"ABC-4","fields":{"parent":{"key":"ABC-1"},"status":{"statusCategory":{"key":"new"}},"customfield_10":5}},
			  {"key":"ABC-5","fields":{"parent":{"key":"ABC-2"},"status":{"statusCategory":{"key":"new"}},
			   "customfield_30":[{"startDate":"2026-10-05T08:00:00.000Z","endDate":"2026-10-19T08:00:00.000Z"},
			                     {"startDate":"2026-09-21T08:00:00.000Z","endDate":"2026-10-05T08:00:00.000Z"}]}}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	epics, err := c.Roadmap(context.Background(), "ABC")
	if err != nil {
		t.Fatal(err)
	}
	if len(epics) != 2 || len(jqls) != 2 || jqls[1] != "parent in (ABC-1,ABC-2)" {
		t.Fatalf("epics %+v, jql %q", epics, jqls)
	}
	e := epics[0]
	if e.Summary != "Checkout" || e.Status != "In Progress" || e.Done || e.DatesFromSprints ||
		e.Start.Format(time.DateOnly) != "2026-09-01" || e.End.Format(time.DateOnly) != "2026-10-15" {
		t.Errorf("ABC-1 = %+v", e)
	}
	if e.Children != 2 || e.DoneChildren != 1 || e.Points != 8 || e.DonePoints != 3 {
		t.Errorf("ABC-1 progress = %+v", e)
	}
	e = epics[1]
	if !e.DatesFromSprints || e.Start.Format(time.DateOnly) != "2026-09-21" || e.End.Format(time.DateOnly) != "2026-10-19" {
		t.Errorf("ABC-2 dates = %v – %v (%v)", e.Start, e.End, e.DatesFromSprints)
	}
}
