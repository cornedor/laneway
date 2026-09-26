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

// TestInbox: others' changes and comments after since, yours and older ones
// left out, mentions first.
func TestInbox(t *testing.T) {
	since := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case "/rest/api/3/search/jql":
			var body struct {
				JQL string `json:"jql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body.JQL, "watcher = currentUser()") || !strings.Contains(body.JQL, "updated >= -") {
				t.Errorf("jql = %s", body.JQL)
			}
			io.WriteString(w, `{"issues":[{"key":"A-1","fields":{"summary":"One"}}]}`)
		case "/rest/api/3/issue/A-1/changelog":
			io.WriteString(w, `{"total":3,"values":[
			  {"author":{"accountId":"bob","displayName":"Bob"},"created":"2026-09-25T09:00:00.000+0000","items":[{"field":"status","fromString":"New","toString":"Old"}]},
			  {"author":{"accountId":"me"},"created":"2026-09-25T11:00:00.000+0000","items":[{"field":"labels","toString":"x"}]},
			  {"author":{"accountId":"bob","displayName":"Bob"},"created":"2026-09-25T12:00:00.000+0000","items":[{"field":"status","fromString":"To Do","toString":"Done"}]}]}`)
		case "/rest/api/3/issue/A-1/comment":
			io.WriteString(w, `{"comments":[
			  {"author":{"accountId":"ann","displayName":"Ann"},"created":"2026-09-25T11:30:00.000+0000",
			   "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"me","text":"@Me"}},{"type":"text","text":" look"}]}]}},
			  {"author":{"accountId":"me"},"created":"2026-09-25T13:00:00.000+0000","body":{"type":"doc","content":[]}}]}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.Inbox(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	if !got[0].Mention || got[0].Who != "Ann" || got[0].What != "mentioned you: Me look" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].What != "status: To Do → Done" || got[1].Summary != "One" {
		t.Errorf("second = %+v", got[1])
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

func TestInboxCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			io.WriteString(w, `{"accountId":"me"}`)
			return
		}
		var body struct {
			JQL string `json:"jql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// JQL rejects a function inside updatedBy(): the accountId goes in.
		if !strings.Contains(body.JQL, "watcher = currentUser()") || !strings.Contains(body.JQL, `issue not in updatedBy("me", "-`) {
			t.Errorf("jql = %s", body.JQL)
		}
		io.WriteString(w, `{"issues":[{"key":"A-1"},{"key":"A-2"}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if n, err := c.InboxCount(context.Background(), time.Now().Add(-time.Hour)); err != nil || n != 2 {
		t.Errorf("n %d, %v", n, err)
	}
}
