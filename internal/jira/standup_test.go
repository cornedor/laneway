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

func TestPreviousWorkday(t *testing.T) {
	for now, want := range map[string]string{
		"2026-09-28": "2026-09-25", // Monday → Friday
		"2026-09-25": "2026-09-24", // Friday → Thursday
		"2026-09-27": "2026-09-25", // Sunday → Friday
	} {
		n, _ := time.Parse(time.DateOnly, now)
		if got := PreviousWorkday(n.Add(15 * time.Hour)).Format(time.DateOnly); got != want {
			t.Errorf("%s: %s, want %s", now, got, want)
		}
	}
}

// TestStandup: your own changes and comments, and your worklogs, oldest
// first; others' left out.
func TestStandup(t *testing.T) {
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(-d).Format(jiraTime) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case r.URL.Path == "/rest/api/3/search/jql":
			var body struct {
				JQL string `json:"jql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.HasPrefix(body.JQL, `issue in updatedBy("me", "-`) {
				io.WriteString(w, `{"issues":[{"key":"A-1","fields":{"summary":"One"}}]}`)
			} else {
				io.WriteString(w, `{"issues":[]}`) // worklogDate searches
			}
		case r.URL.Path == "/rest/api/3/issue/A-1/changelog":
			io.WriteString(w, `{"total":2,"values":[
			  {"author":{"accountId":"me"},"created":"`+at(2*time.Hour)+`","items":[{"field":"status","fromString":"To Do","toString":"Done"}]},
			  {"author":{"accountId":"bob"},"created":"`+at(time.Hour)+`","items":[{"field":"labels","toString":"x"}]}]}`)
		case r.URL.Path == "/rest/api/3/issue/A-1/comment":
			io.WriteString(w, `{"comments":[{"author":{"accountId":"me"},"created":"`+at(3*time.Hour)+`",
			  "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"on it"}]}]}}]}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.Standup(context.Background(), now.Add(-5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].What != "commented: on it" || got[1].What != "status: To Do → Done" {
		t.Errorf("entries = %+v", got)
	}
}
