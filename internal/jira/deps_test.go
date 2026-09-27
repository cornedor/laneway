package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDependencies: blockers followed deep, a cycle stopped, what it
// blocks on the other side; other link types ignored.
func TestDependencies(t *testing.T) {
	issue := func(key, status, links string) string {
		return `{"key":"` + key + `","fields":{"summary":"` + key + ` s","status":{"name":"` + status + `","statusCategory":{"key":"new"}},"issuelinks":[` + links + `]}}`
	}
	blockedBy := func(k string) string {
		return `{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"` + k + `","fields":{"summary":"` + k + ` s","status":{"name":"To Do"}}}}`
	}
	blocks := func(k string) string {
		return `{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"outwardIssue":{"key":"` + k + `","fields":{"summary":"` + k + ` s","status":{"name":"To Do"}}}}`
	}
	relates := `{"type":{"name":"Relates","inward":"relates to","outward":"relates to"},"outwardIssue":{"key":"A-9","fields":{"summary":"x"}}}`
	pages := map[string]string{
		"A-1": issue("A-1", "In Progress", blockedBy("A-2")+","+blocks("A-5")+","+relates),
		"A-2": issue("A-2", "To Do", blockedBy("A-3")),
		"A-3": issue("A-3", "To Do", blockedBy("A-1")),
		"A-5": issue("A-5", "To Do", ""),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, pages[strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")])
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	root, by, bl, err := c.Dependencies(context.Background(), "A-1")
	if err != nil {
		t.Fatal(err)
	}
	if root.Status != "In Progress" || len(by) != 1 || by[0].Key != "A-2" || by[0].Kids[0].Key != "A-3" || !by[0].Kids[0].Kids[0].Seen {
		t.Errorf("blocked by %+v", by)
	}
	if len(bl) != 1 || bl[0].Key != "A-5" || len(bl[0].Kids) != 0 {
		t.Errorf("blocks %+v", bl)
	}
}
