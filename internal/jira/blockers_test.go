package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestBlockers: only open "is blocked by" links count.
func TestBlockers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"issues": [
			{"key": "ABC-1", "fields": {"issuelinks": [
				{"type": {"inward": "is blocked by"}, "inwardIssue": {"key": "ABC-9", "fields": {"status": {"statusCategory": {"key": "indeterminate"}}}}},
				{"type": {"inward": "is blocked by"}, "inwardIssue": {"key": "ABC-8", "fields": {"status": {"statusCategory": {"key": "done"}}}}},
				{"type": {"inward": "is blocked by"}, "outwardIssue": {"key": "ABC-7"}},
				{"type": {"inward": "relates to"}, "inwardIssue": {"key": "ABC-6", "fields": {}}}
			]}},
			{"key": "ABC-2", "fields": {}}
		]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.Blockers(context.Background(), []string{"ABC-1", "ABC-2"})
	if err != nil || len(got) != 1 || !slices.Equal(got["ABC-1"], []string{"ABC-9"}) {
		t.Errorf("Blockers = %v, %v", got, err)
	}
}
