package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// TestAllIssuesCards: the first ask starts reading the project whole and
// says so; asked again once it's read, every issue comes, open and closed,
// and the quick filters' JQL narrows them. Without an index there is no
// such view.
func TestAllIssuesCards(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(t.TempDir(), "ix.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, Jira: config.JiraConfig{}, Store: st, Site: "demo", Demo: true, Index: ix}))
	t.Cleanup(ts.Close)
	type answer struct {
		Cards []jira.Card
		Total int
		Sync  *syncOut
	}
	var a answer
	u := ts.URL + "/api/boards/1/cards?allissues=1&project=DEMO"
	if code := issueCall(t, "GET", u, nil, &a); code != 200 || a.Sync == nil {
		t.Fatalf("first ask: %d %+v; want a sync under way", code, a)
	}
	for deadline := time.Now().Add(5 * time.Second); a.Sync != nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		a = answer{}
		issueCall(t, "GET", u+"&fresh=1", nil, &a)
	}
	done := 0
	for _, c := range a.Cards {
		if c.Done {
			done++
		}
	}
	if a.Sync != nil || a.Total < 20 || done == 0 || done == len(a.Cards) {
		t.Fatalf("after the read: sync %+v, %d cards, %d done; want every issue, open and closed", a.Sync, a.Total, done)
	}
	var bugs answer
	issueCall(t, "GET", u+"&jql="+"issuetype%20%3D%20Bug", nil, &bugs)
	if bugs.Total == 0 || bugs.Total >= a.Total {
		t.Errorf("filtered by type = Bug: %d of %d", bugs.Total, a.Total)
	}
	for _, c := range bugs.Cards {
		if c.Type != "Bug" {
			t.Errorf("%s is a %s", c.Key, c.Type)
		}
	}
	if code := issueCall(t, "GET", ts.URL+"/api/boards/1/cards?allissues=1&project=DE%22MO", nil, nil); code != 400 {
		t.Errorf("a bad project key: %d, want 400", code)
	}
	noIx := httptest.NewServer(New(context.Background(), Options{Client: cl, Jira: config.JiraConfig{}, Store: st, Site: "demo", Demo: true}))
	t.Cleanup(noIx.Close)
	var b map[string]any
	issueCall(t, "GET", noIx.URL+"/api/boards/1", nil, &b)
	if b["allIssues"] != false {
		t.Errorf("without an index allIssues = %v", b["allIssues"])
	}
}
