package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

func TestOptionsFrom(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{})
	if !reflect.DeepEqual(o, defaultOptions()) || len(warn) != 0 {
		t.Fatalf("empty config = %+v %v, want defaults", o, warn)
	}
	o, warn = optionsFrom(config.UIConfig{AutoRefresh: "off", StaleAfter: "30s", Images: "off", ImageMaxRows: 8, PanelWidth: 40})
	want := defaultOptions()
	want.autoRefresh, want.staleAfter, want.images, want.imageMaxRows, want.panelPct = 0, 30*time.Second, false, 8, 40
	if !reflect.DeepEqual(o, want) || len(warn) != 0 {
		t.Errorf("set = %+v %v, want %+v", o, warn, want)
	}
	o, warn = optionsFrom(config.UIConfig{AutoRefresh: "soon", StaleAfter: "1s", Images: "maybe", ImageMaxRows: -1, PanelWidth: 95})
	if !reflect.DeepEqual(o, defaultOptions()) || len(warn) != 5 {
		t.Errorf("bad values = %+v, %d warnings %v; want defaults and 5", o, len(warn), warn)
	}
}

func TestAutoRefreshOff(t *testing.T) {
	m := jiraTabModel(t)
	m.opts.autoRefresh = 0
	if m.jiraAutoRefreshTick() != nil {
		t.Error("tick armed with auto_refresh off")
	}
}

func TestPanelWidthOption(t *testing.T) {
	if got := splitRightPane(200, 30); got != 60 {
		t.Errorf("30%% of 200 = %d", got)
	}
}

func TestOptionsModeDateFields(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{DefaultMode: "list", DateFormat: "02 Jan", CardFields: []string{"points", "Parent", "colour"}})
	if o.lanes || o.dateFormat != "02 Jan" || len(warn) != 1 {
		t.Errorf("mode/date = %v %q, warnings %v", o.lanes, o.dateFormat, warn)
	}
	if o.fields != (cardFields{points: true, parent: true}) {
		t.Errorf("fields = %+v", o.fields)
	}
	if _, warn := optionsFrom(config.UIConfig{DefaultMode: "grid"}); len(warn) != 1 {
		t.Error("bad mode not reported")
	}
}

func TestCardFieldsHideAssignee(t *testing.T) {
	c := jira.Card{Key: "ABC-1", Summary: "s", Assignee: "Ada", Points: "3", ParentSummary: "Epic"}
	got := jiraCardLines(c, false, cardFields{parent: true})
	if got[0] != "ABC-1" || got[2] != "⌃ Epic" {
		t.Errorf("lines = %q", got)
	}
	if got := jiraCardLines(c, false, allCardFields); got[0] != "ABC-1 3" || got[2] != "Ada · ⌃ Epic" {
		t.Errorf("all fields = %q", got)
	}
}

func TestDefaultModeList(t *testing.T) {
	m := jiraTabModel(t)
	if !m.jiraTab.wantLanes {
		t.Fatal("default should be lanes")
	}
	opts, _ := optionsFrom(config.UIConfig{DefaultMode: "list"})
	m2 := New(m.ctx, config.JiraConfig{}, config.UIConfig{DefaultMode: "list"}, m.store)
	if m2.jiraTab.wantLanes || opts.lanes {
		t.Error("default_mode list ignored")
	}
}

func TestLocalQuickFilters(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{QuickFilters: []config.QuickFilter{{Name: "Bugs", JQL: "type = Bug"}, {Name: "x"}}})
	if len(o.quick) != 1 || o.quick[0].ID != -1 || len(warn) != 1 {
		t.Fatalf("quick = %+v, warn %v", o.quick, warn)
	}
	board := []jira.QuickFilter{{ID: 7, Name: "FE"}}
	got := withLocalQuick(withLocalQuick(board, o.quick), o.quick)
	if len(got) != 2 || got[0].ID != -1 || got[1].ID != 7 {
		t.Errorf("merged = %+v, want local first, no duplicates", got)
	}
	if jql := jiraFilterJQL(jiraAssignee{}, got, map[int]bool{-1: true}); jql != "(type = Bug)" {
		t.Errorf("jql = %q", jql)
	}
}

func TestLocalViews(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{Views: []config.QuickFilter{{Name: "Mine", JQL: "assignee = currentUser()"}, {JQL: "x"}}})
	if len(o.views) != 1 || o.views[0].kind != jiraViewJQL || !o.views[0].lanes || len(warn) != 1 {
		t.Fatalf("views = %+v, warn %v", o.views, warn)
	}
	var gotPath, gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotJQL = r.URL.Path, r.URL.Query().Get("jql")
		_, _ = w.Write([]byte(`{"total":0,"issues":[]}`))
	}))
	defer srv.Close()
	c := jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if _, _, err := fetchJiraView(context.Background(), c, 7, &jira.BoardConfig{}, o.views[0], "type = Bug"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rest/agile/1.0/board/7/issue" || gotJQL != "(assignee = currentUser()) AND (type = Bug)" {
		t.Errorf("request = %s jql %q", gotPath, gotJQL)
	}
	back := cacheOf(jiraBoardMsg{views: o.views}, "").boardMsg(0).views
	if len(back) != 1 || back[0] != o.views[0] {
		t.Errorf("cache round trip = %+v", back)
	}
}

func TestCardLimitOption(t *testing.T) {
	if o, warn := optionsFrom(config.UIConfig{CardLimit: 1500}); o.cardLimit != 1500 || len(warn) != 0 {
		t.Errorf("1500 = %d %v", o.cardLimit, warn)
	}
	if o, warn := optionsFrom(config.UIConfig{CardLimit: 10}); o.cardLimit != 0 || len(warn) != 1 {
		t.Errorf("10 = %d %v", o.cardLimit, warn)
	}
}
