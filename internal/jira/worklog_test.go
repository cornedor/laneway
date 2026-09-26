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

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]struct {
		secs int
		rest string
	}{
		"1h 30m fixed the tests": {5400, "fixed the tests"},
		"1h30m":                  {5400, ""},
		"1.5h review":            {5400, "review"},
		"45m":                    {2700, ""},
		"2d":                     {57600, ""},
		"10M":                    {600, ""},
	} {
		secs, rest, err := ParseDuration(in)
		if err != nil || secs != want.secs || rest != want.rest {
			t.Errorf("%q = %d %q %v", in, secs, rest, err)
		}
	}
	for _, in := range []string{"", "fixed it", "h", "1x"} {
		if _, _, err := ParseDuration(in); err == nil {
			t.Errorf("%q should fail", in)
		}
	}
	for secs, want := range map[int]string{60: "1m", 3600: "1h", 5430: "1h 31m", 7170: "2h"} {
		if got := FormatDuration(secs); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestAddWorklog(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/ABC-1/worklog" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	started := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	if err := c.AddWorklog(context.Background(), "ABC-1", 5400, started, "tests"); err != nil {
		t.Fatal(err)
	}
	if body["timeSpentSeconds"] != 5400.0 || body["started"] != "2026-09-25T09:00:00.000+0000" || body["comment"] == nil {
		t.Errorf("body = %v", body)
	}
}

// TestMyWorklogs: your own entries of the day, across issues, by start.
func TestMyWorklogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case r.URL.Path == "/rest/api/3/search/jql":
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `worklogDate = \"2026-09-25\"`) {
				t.Errorf("jql body %s", b)
			}
			io.WriteString(w, `{"issues":[{"key":"A-1","fields":{"summary":"One"}},{"key":"A-2","fields":{"summary":"Two"}}]}`)
		case r.URL.Path == "/rest/api/3/issue/A-1/worklog":
			io.WriteString(w, `{"worklogs":[
			  {"author":{"accountId":"me"},"started":"2026-09-25T14:00:00.000+0200","timeSpentSeconds":3600},
			  {"author":{"accountId":"other"},"started":"2026-09-25T10:00:00.000+0200","timeSpentSeconds":600}]}`)
		case r.URL.Path == "/rest/api/3/issue/A-2/worklog":
			io.WriteString(w, `{"worklogs":[{"author":{"accountId":"me"},"started":"2026-09-25T09:00:00.000+0200","timeSpentSeconds":1800}]}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	loc := time.FixedZone("CEST", 2*3600)
	logs, err := c.MyWorklogs(context.Background(), time.Date(2026, 9, 25, 12, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Key != "A-2" || logs[1].Key != "A-1" || logs[1].Seconds != 3600 || logs[1].Summary != "One" {
		t.Errorf("logs = %+v", logs)
	}
}

func TestIssueWorklogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/A-1/worklog" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"worklogs":[
			{"id":"2","author":{"displayName":"Bob"},"started":"2026-09-25T13:00:00.000+0000","timeSpentSeconds":1800},
			{"id":"1","author":{"displayName":"Ann"},"started":"2026-09-25T09:00:00.000+0000","timeSpentSeconds":3600,
			 "comment":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"review"}]}]}}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.IssueWorklogs(context.Background(), "A-1")
	if err != nil || len(got) != 2 || got[0].Author != "Ann" || got[0].Comment != "review" || got[1].Seconds != 1800 {
		t.Errorf("%+v, %v", got, err)
	}
}
