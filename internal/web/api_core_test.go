package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

func TestKanbanCards(t *testing.T) {
	var jql string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/agile/1.0/board/7/configuration":
			fmt.Fprint(w, `{"columnConfig":{"columns":[{"name":"Backlog","statuses":[{"id":"1"}]},{"name":"Doing","statuses":[{"id":"3"}]}]}}`)
		case "/rest/agile/1.0/board/7/issue":
			jql = r.URL.Query().Get("jql")
			fmt.Fprint(w, `{"total":2,"issues":[{"key":"K-1","fields":{"status":{"id":"1"}}},{"key":"K-2","fields":{"status":{"id":"3"}}}]}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(fake.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cl := jira.New(jira.Config{BaseURL: fake.URL, Email: "d@example.com", APIToken: "x"})
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, Jira: config.JiraConfig{}, UI: config.UIConfig{KanbanDoneDays: 30}, Store: st, Site: "t"}))
	t.Cleanup(ts.Close)

	var out struct {
		Cards []jira.Card
		Total int
	}
	if code := issueCall(t, "GET", ts.URL+"/api/boards/7/cards?kanban=1&jql=assignee%20%3D%20x", nil, &out); code != 200 {
		t.Fatalf("status %d", code)
	}
	if want := "(statusCategory != Done OR updated >= -30d) AND (assignee = x)"; jql != want {
		t.Errorf("jql = %q, want %q", jql, want)
	}
	if len(out.Cards) != 1 || out.Cards[0].Key != "K-2" || out.Total != 1 {
		t.Errorf("backlog column not left out: %+v total %d", out.Cards, out.Total)
	}
	if issueCall(t, "GET", ts.URL+"/api/boards/7/cards", nil, nil); strings.Contains(jql, "updated") {
		t.Errorf("whole board filtered: %q", jql)
	}
}

func TestVelocitySprints(t *testing.T) {
	for n, want := range map[int]int{0: 8, 3: 3, 50: 50, 51: 8, -1: 8} {
		if got := velocitySprints(config.UIConfig{VelocitySprints: n}); got != want {
			t.Errorf("velocitySprints(%d) = %d, want %d", n, got, want)
		}
	}
}
