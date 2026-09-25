package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestVelocity: the last n closed sprints across pages; done counts only
// what was resolved by the sprint's completion.
func TestVelocity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/agile/1.0/board/1/sprint":
			if r.URL.Query().Get("startAt") == "0" {
				io.WriteString(w, `{"isLast":false,"values":[{"id":1,"name":"S1"},{"id":2,"name":"S2"}]}`)
				return
			}
			io.WriteString(w, `{"isLast":true,"values":[{"id":3,"name":"S3","completeDate":"2026-09-19T16:00:00.000Z"}]}`)
		case "/rest/api/3/search/jql":
			var body struct {
				JQL string `json:"jql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body.JQL {
			case "sprint = 3":
				io.WriteString(w, `{"issues":[
				  {"key":"A-1","fields":{"customfield_1":5,"resolutiondate":"2026-09-18T10:00:00.000+0200"}},
				  {"key":"A-2","fields":{"customfield_1":3,"resolutiondate":"2026-09-22T10:00:00.000+0200"}},
				  {"key":"A-3","fields":{"customfield_1":2}}]}`)
			default:
				io.WriteString(w, `{"issues":[{"key":"A-9","fields":{"customfield_1":1}}]}`)
			}
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	v, err := c.Velocity(context.Background(), 1, 2, "customfield_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 2 || v[0].Name != "S2" || v[1].Name != "S3" {
		t.Fatalf("sprints = %+v", v)
	}
	if v[1].Committed != 10 || v[1].Done != 5 {
		t.Errorf("S3 = %+v", v[1])
	}
}
