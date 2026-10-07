package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
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

func TestBoardLocalQuickFilters(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x"})
	ui := config.UIConfig{QuickFilters: []config.QuickFilter{{Name: "Bugs", JQL: "type = Bug"}, {Name: "no jql"}}}
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, UI: ui, Store: st, Site: "demo", Demo: true}))
	t.Cleanup(ts.Close)

	var out struct{ QuickFilters []jira.QuickFilter }
	if code := issueCall(t, "GET", ts.URL+"/api/boards/1", nil, &out); code != 200 {
		t.Fatalf("status %d", code)
	}
	q := out.QuickFilters
	if len(q) < 2 || q[0] != (jira.QuickFilter{ID: -1, Name: "Bugs", JQL: "type = Bug"}) || q[1].ID < 0 {
		t.Errorf("quick filters = %+v, want the preset first, then the board's", q)
	}

	ui.BoardQuickFilters = "off"
	ts = httptest.NewServer(New(context.Background(), Options{Client: cl, UI: ui, Store: st, Site: "demo", Demo: true}))
	t.Cleanup(ts.Close)
	out.QuickFilters = nil
	if code := issueCall(t, "GET", ts.URL+"/api/boards/1", nil, &out); code != 200 {
		t.Fatalf("status %d", code)
	}
	if q := out.QuickFilters; len(q) != 1 || q[0].ID != -1 {
		t.Errorf("board_quick_filters off: quick filters = %+v, want the preset alone", q)
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SetMeta("jira:mode", "web:x")
	ts := httptest.NewServer(New(context.Background(), Options{Client: jira.New(jira.Config{}), Store: st, Site: "t"}))
	t.Cleanup(ts.Close)

	if code := issueCall(t, "PUT", ts.URL+"/api/prefs/theme.preset", map[string]string{"Value": "nord"}, nil); code >= 300 {
		t.Fatalf("put status %d", code)
	}
	var out map[string]string
	if code := issueCall(t, "GET", ts.URL+"/api/prefs", nil, &out); code != 200 {
		t.Fatalf("get status %d", code)
	}
	if len(out) != 1 || out["theme.preset"] != "nord" {
		t.Errorf("prefs = %v, want only theme.preset=nord", out)
	}
}
