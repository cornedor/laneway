package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeIndex struct {
	cards  []string
	issues []string
}

func (f *fakeIndex) PutCards(cs []Card) {
	for _, c := range cs {
		f.cards = append(f.cards, c.Key)
	}
}

func (f *fakeIndex) PutIssue(iss *Issue) { f.issues = append(f.issues, iss.Key) }

// TestIndexMirrorsReads: board loads, searches and panel reads reach the
// index; a cached panel read doesn't again.
func TestIndexMirrorsReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			fmt.Fprint(w, `{"issues": [{"key": "ABC-1", "fields": {"summary": "One"}}]}`)
		case strings.HasPrefix(r.URL.Path, "/rest/agile/1.0/board/1/issue"):
			fmt.Fprint(w, `{"total": 1, "issues": [{"key": "ABC-2", "fields": {"summary": "Two"}}]}`)
		case r.URL.Path == "/rest/api/3/issue/ABC-3":
			fmt.Fprint(w, `{"key": "ABC-3", "fields": {"summary": "Three"}}`)
		case r.URL.Path == "/rest/api/3/field":
			fmt.Fprint(w, `[]`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	ix := &fakeIndex{}
	c.SetIndex(ix)
	ctx := context.Background()
	if _, err := c.SearchCards(ctx, "project = ABC"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.BoardIssues(ctx, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := c.Get(ctx, "ABC-3"); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(ix.cards, " ") != "ABC-1 ABC-2" || strings.Join(ix.issues, " ") != "ABC-3" {
		t.Errorf("indexed cards %q, issues %q", ix.cards, ix.issues)
	}
}
