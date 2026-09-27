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

// TestMoveIssue: the move is submitted with every default inferred, polled
// to its end, and the issue read back under its new key.
func TestMoveIssue(t *testing.T) {
	movePoll = time.Millisecond
	polls := 0
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/bulk/issues/move":
			_ = json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"taskId":"77"}`)
		case "/rest/api/3/bulk/queue/77":
			if polls++; polls < 2 {
				io.WriteString(w, `{"status":"RUNNING"}`)
				return
			}
			io.WriteString(w, `{"status":"COMPLETE","processedAccessibleIssues":[10001]}`)
		case "/rest/api/3/issue/ABC-1":
			io.WriteString(w, `{"key":"XYZ-5"}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	key, err := c.MoveIssue(context.Background(), "ABC-1", "XYZ", "10002")
	if err != nil || key != "XYZ-5" || polls != 2 {
		t.Fatalf("key %q, err %v, polls %d", key, err, polls)
	}
	m, _ := body["targetToSourcesMapping"].(map[string]any)["XYZ,10002"].(map[string]any)
	if m == nil || m["inferStatusDefaults"] != true || m["issueIdsOrKeys"].([]any)[0] != "ABC-1" {
		t.Errorf("body = %v", body)
	}
}

// TestMoveIssueFails: Jira's reason for a failed issue is the error.
func TestMoveIssueFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/move") {
			io.WriteString(w, `{"taskId":"1"}`)
			return
		}
		io.WriteString(w, `{"status":"COMPLETE","failedAccessibleIssues":{"10001":["Summary is required"]}}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if _, err := c.MoveIssue(context.Background(), "ABC-1", "XYZ", "1"); err == nil || err.Error() != "Summary is required" {
		t.Errorf("err = %v", err)
	}
}

// TestMoveTypes: a subtask has nowhere to go on its own.
func TestMoveTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"issueTypes":[{"id":"1","name":"Task"},{"id":"5","name":"Sub-task","subtask":true}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if types, err := c.MoveTypes(context.Background(), "ABC", "Task", "XYZ"); err != nil || len(types) != 1 || types[0].Name != "Task" {
		t.Errorf("task: %v, %v", types, err)
	}
	if _, err := c.MoveTypes(context.Background(), "ABC", "Sub-task", "XYZ"); err == nil {
		t.Error("a subtask should not move alone")
	}
}

// TestDeleteIssue: subtasks go along only when asked.
func TestDeleteIssue(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	_ = c.DeleteIssue(context.Background(), "ABC-1", true)
	_ = c.DeleteIssue(context.Background(), "ABC-2", false)
	if len(got) != 2 || got[0] != "DELETE /rest/api/3/issue/ABC-1?deleteSubtasks=true" || got[1] != "DELETE /rest/api/3/issue/ABC-2?deleteSubtasks=false" {
		t.Errorf("requests = %q", got)
	}
}

// TestWebLinks: remote links read with their app, and one added titled by
// its URL when no title is given.
func TestWebLinks(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			posted = string(b)
			w.WriteHeader(http.StatusCreated)
			return
		}
		io.WriteString(w, `[{"object":{"url":"https://wiki.test/spec","title":"Spec"},"application":{"name":"Confluence"}},{"object":{"url":"https://x.test"}}]`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	links, err := c.WebLinks(context.Background(), "ABC-1")
	if err != nil || len(links) != 2 || links[0] != (WebLink{"Spec", "https://wiki.test/spec", "Confluence"}) || links[1].Title != "https://x.test" {
		t.Fatalf("links %+v, %v", links, err)
	}
	if err := c.AddWebLink(context.Background(), "ABC-1", "https://y.test", ""); err != nil || posted != `{"object":{"title":"https://y.test","url":"https://y.test"}}` {
		t.Errorf("posted %s, %v", posted, err)
	}
}
