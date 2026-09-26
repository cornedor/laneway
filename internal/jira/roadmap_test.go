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
				   "customfield_20":"2026-09-01","duedate":"2026-10-15","parent":{"key":"ABC-100","fields":{"summary":"Grow"}}}},
				  {"key":"ABC-2","fields":{"summary":"Search","status":{"name":"To Do","statusCategory":{"key":"new"}},
				   "issuelinks":[{"type":{"inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"ABC-1"}},
				                 {"type":{"inward":"relates to","outward":"relates to"},"inwardIssue":{"key":"ABC-9"}}]}}]}`)
				return
			}
			io.WriteString(w, `{"issues":[
			  {"key":"ABC-3","fields":{"summary":"Pay","issuetype":{"name":"Story"},"duedate":"2026-09-20","parent":{"key":"ABC-1"},"status":{"statusCategory":{"key":"done"}},"customfield_10":3}},
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
	epics, err := c.Roadmap(context.Background(), "ABC", "Epic", 90)
	if err != nil {
		t.Fatal(err)
	}
	if len(epics) != 2 || len(jqls) != 2 || jqls[1] != "parent in (ABC-1,ABC-2) ORDER BY rank" ||
		jqls[0] != `project = "ABC" AND issuetype = "Epic" AND (statusCategory != Done OR resolved >= -90d) ORDER BY rank` {
		t.Fatalf("epics %+v, jql %q", epics, jqls)
	}
	e := epics[0]
	if e.Summary != "Checkout" || e.Status != "In Progress" || e.Done || e.DatesFromSprints ||
		e.Start.Format(time.DateOnly) != "2026-09-01" || e.End.Format(time.DateOnly) != "2026-10-15" {
		t.Errorf("ABC-1 = %+v", e)
	}
	if e.Parent != "ABC-100" || e.ParentSummary != "Grow" || epics[1].Parent != "" {
		t.Errorf("parents = %q %q, %q", e.Parent, e.ParentSummary, epics[1].Parent)
	}
	if e.Children != 2 || e.DoneChildren != 1 || e.Points != 8 || e.DonePoints != 3 {
		t.Errorf("ABC-1 progress = %+v", e)
	}
	if k := e.Kids; len(k) != 2 || k[0].Summary != "Pay" || k[0].Type != "Story" || !k[0].Done ||
		k[0].End.Format(time.DateOnly) != "2026-09-20" || k[1].Key != "ABC-4" {
		t.Errorf("ABC-1 kids = %+v", k)
	}
	if b := epics[1].BlockedBy; len(b) != 1 || b[0] != "ABC-1" {
		t.Errorf("ABC-2 blocked by %v", b)
	}
	e = epics[1]
	if k := e.Kids; len(k) != 1 || !k[0].DatesFromSprints || k[0].Start.Format(time.DateOnly) != "2026-09-21" {
		t.Errorf("ABC-2 kids = %+v", k)
	}
	if !e.DatesFromSprints || e.Start.Format(time.DateOnly) != "2026-09-21" || e.End.Format(time.DateOnly) != "2026-10-19" {
		t.Errorf("ABC-2 dates = %v – %v (%v)", e.Start, e.End, e.DatesFromSprints)
	}
}

// TestSetDates: start into the instance's start field, end into the due date.
func TestSetDates(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			io.WriteString(w, `[{"id":"customfield_20","name":"Start date"}]`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = r.Method + " " + r.URL.Path + " " + string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	if err := c.SetDates(context.Background(), "ABC-1", start, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if body != `PUT /rest/api/3/issue/ABC-1 {"fields":{"customfield_20":"2026-10-01","duedate":null}}` {
		t.Errorf("request = %s", body)
	}
}
