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

// TestInboxIssues: your issues with their status, assignee and updated.
func TestInboxIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			JQL string `json:"jql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.JQL, "watcher = currentUser()") || !strings.Contains(body.JQL, "updated >= -") {
			t.Errorf("jql = %s", body.JQL)
		}
		io.WriteString(w, `{"issues":[{"key":"A-1","fields":{"summary":"One","status":{"name":"Done"},
		  "assignee":{"displayName":"Ann"},"updated":"2026-09-25T12:00:00.000+0000"}}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.InboxIssues(context.Background(), time.Now().Add(-time.Hour))
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
	if is := got[0]; is.Key != "A-1" || is.Summary != "One" || is.Status != "Done" || is.Assignee != "Ann" || is.Updated.IsZero() {
		t.Errorf("issue = %+v", is)
	}
}

// TestIssueInbox: others' changes and comments after since, oldest first;
// yours, older ones and Rank changes left out; a mention and an assignment
// to you marked, a comment's id and body kept for a reply.
func TestIssueInbox(t *testing.T) {
	since := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case "/rest/api/3/issue/A-1/changelog":
			io.WriteString(w, `{"total":4,"values":[
			  {"author":{"accountId":"bob","displayName":"Bob"},"created":"2026-09-25T09:00:00.000+0000","items":[{"field":"status","fromString":"New","toString":"Old"}]},
			  {"author":{"accountId":"me"},"created":"2026-09-25T11:00:00.000+0000","items":[{"field":"labels","toString":"x"}]},
			  {"author":{"accountId":"bob","displayName":"Bob"},"created":"2026-09-25T11:10:00.000+0000","items":[{"field":"Rank","toString":"Ranked higher"}]},
			  {"author":{"accountId":"bob","displayName":"Bob"},"created":"2026-09-25T12:00:00.000+0000","items":[
			    {"field":"status","fromString":"To Do","toString":"Done"},{"field":"assignee","fromString":"Bob","toString":"Me","to":"me"}]}]}`)
		case "/rest/api/3/issue/A-1/comment":
			io.WriteString(w, `{"comments":[
			  {"id":"77","author":{"accountId":"ann","displayName":"Ann"},"created":"2026-09-25T11:30:00.000+0000",
			   "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"me","text":"@Me"}},{"type":"text","text":" look"}]}]}},
			  {"author":{"accountId":"me"},"created":"2026-09-25T13:00:00.000+0000","body":{"type":"doc","content":[]}}]}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.IssueInbox(context.Background(), "A-1", "One", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	if e := got[0]; !e.Mention || e.Who != "Ann" || e.WhoID != "ann" || e.CommentID != "77" || e.Body != "@Me look" || e.What != "mentioned you: @Me look" {
		t.Errorf("first = %+v", e)
	}
	if e := got[1]; !e.Assigned || e.What != "status: To Do → Done · assignee: Bob → Me" || e.Summary != "One" {
		t.Errorf("second = %+v", e)
	}
}

func TestHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/A-1/changelog":
			io.WriteString(w, `{"total":1,"values":[{"author":{"displayName":"Bob"},"created":"2026-09-25T09:00:00.000+0000","items":[{"field":"status","fromString":"To Do","toString":"Done"}]}]}`)
		case "/rest/api/3/issue/A-1/comment":
			io.WriteString(w, `{"comments":[{"author":{"displayName":"Ann"},"created":"2026-09-25T10:00:00.000+0000","body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"ok"}]}]}}]}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.History(context.Background(), "A-1")
	if err != nil || len(got) != 2 || got[0].Who != "Ann" || got[1].What != "status: To Do → Done" {
		t.Errorf("%+v, %v", got, err)
	}
}

func TestChangelog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/A-1/changelog" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"total":2,"values":[
			{"author":{"displayName":"Bob"},"created":"2026-09-25T09:00:00.000+0000","items":[{"field":"status","fromString":"To Do","toString":"Done"}]},
			{"author":{"displayName":"Ann"},"created":"2026-09-25T10:00:00.000+0000","items":[{"field":"labels","toString":"ui"}]}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.Changelog(context.Background(), "A-1")
	if err != nil || len(got) != 2 || got[0].Who != "Bob" || got[1].What != "labels: — → ui" {
		t.Errorf("%+v, %v", got, err)
	}
}

func TestInboxCap(t *testing.T) {
	for in, want := range map[int]int{0: 30, -3: 30, 50: 50} {
		if c := New(Config{InboxIssues: in}); c.inboxCap != want {
			t.Errorf("InboxIssues %d: cap %d, want %d", in, c.inboxCap, want)
		}
	}
}

// TestChangelogWorklog: logging work reads as "logged 1h", not its raw
// timespent and WorklogId items; removing it as "-1h".
func TestChangelogWorklog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"total":2,"values":[
			{"author":{"displayName":"Bob"},"created":"2026-09-25T09:00:00.000+0000","items":[
				{"field":"timespent","fromString":"3600","toString":"7200"},
				{"field":"timeestimate","fromString":"7200","toString":"3600"},
				{"field":"WorklogId","toString":"10042"}]},
			{"author":{"displayName":"Bob"},"created":"2026-09-25T10:00:00.000+0000","items":[
				{"field":"timespent","fromString":"7200","toString":"3600"},
				{"field":"WorklogId","fromString":"10042"}]}]}`)
	}))
	defer srv.Close()
	got, err := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"}).Changelog(context.Background(), "A-1")
	if err != nil || len(got) != 2 || got[0].What != "logged 1h" || got[1].What != "logged -1h" || len(got[0].Changes) != 1 {
		t.Errorf("%+v, %v", got, err)
	}
	if kept := dropWorklogChanges(got); len(kept) != 0 {
		t.Errorf("standup kept %+v", kept)
	}
}
