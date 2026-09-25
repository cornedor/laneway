package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChangeAuthor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/A-1" && r.URL.Query().Get("fields") == "creator":
			fmt.Fprint(w, `{"fields":{"creator":{"accountId":"maker"}}}`)
		case r.URL.Path == "/rest/api/3/issue/A-1/changelog" && r.URL.Query().Get("maxResults") == "1":
			fmt.Fprint(w, `{"total":120,"values":[{"author":{"accountId":"old"},"items":[{"fieldId":"status"}]}]}`)
		case r.URL.Path == "/rest/api/3/issue/A-1/changelog" && r.URL.Query().Get("startAt") == "70":
			fmt.Fprint(w, `{"total":120,"values":[
				{"author":{"accountId":"ada"},"items":[{"fieldId":"status"}]},
				{"author":{"accountId":"bob"},"items":[{"fieldId":"status"},{"fieldId":"assignee"}]},
				{"author":{"accountId":"cy"},"items":[{"fieldId":"labels"}]}]}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	for field, want := range map[string]string{"": "maker", "status": "bob", "assignee": "bob", "priority": ""} {
		if got, err := c.ChangeAuthor(context.Background(), "A-1", field); err != nil || got != want {
			t.Errorf("%q = %q %v, want %q", field, got, err, want)
		}
	}
}
