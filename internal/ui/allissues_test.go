package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// allIssuesModel shows the all issues view of a board whose columns know
// statuses 1, 3 and 5: the cards are out of updated order, one done with
// Won't Do, one in status 9 that no column shows.
func allIssuesModel(t *testing.T) Model {
	t.Helper()
	m := configuredJiraModel(t, "ABC")
	m.width, m.height = 160, 40
	m.resize()
	day := func(d int) time.Time { return time.Now().Add(-time.Duration(d) * 24 * time.Hour) }
	out, _ := m.handleJiraBoard(jiraBoardMsg{
		seq: m.jiraTab.seq, project: "ABC",
		boards: []jira.Board{{ID: 1, Name: "ABC board", Type: "scrum"}},
		cfg: &jira.BoardConfig{Columns: []jira.Column{
			{Name: "To do", StatusIDs: []string{"1"}},
			{Name: "In progress", StatusIDs: []string{"3"}},
			{Name: "Done", StatusIDs: []string{"5"}},
		}},
		views:   []jiraView{{kind: jiraViewSprint, name: "Sprint 1", sprint: 9, lanes: true}, allIssuesView()},
		viewIdx: 1,
		cards: []jira.Card{
			{Key: "ABC-1", Summary: "Old", StatusID: "1", Status: "New", Updated: day(30)},
			{Key: "ABC-2", Summary: "Fresh", StatusID: "3", Status: "In progress", Updated: day(1)},
			{Key: "ABC-3", Summary: "Dropped", StatusID: "5", Status: "Closed", Done: true, Resolution: "Won't Do", Resolved: day(3), Updated: day(3)},
			{Key: "ABC-4", Summary: "Parked", StatusID: "9", Status: "Parked", Updated: day(10)},
		},
		total: 4,
	})
	return out.(Model)
}

// TestAllIssuesView: a list only, of every issue, also the ones in a status
// the board has no column for; most recently updated first, and the title
// row counts them.
func TestAllIssuesView(t *testing.T) {
	m := allIssuesModel(t)
	if !m.inAllIssues() {
		t.Fatal("not on the all issues view")
	}
	if m.jiraTab.wantLanes = true; m.jiraShowsLanes() {
		t.Error("all issues shows as lanes; want a list whatever the mode")
	}
	var keys []string
	for _, i := range m.jiraTab.order {
		keys = append(keys, m.jiraTab.cards[i].Key)
	}
	if got := strings.Join(keys, " "); got != "ABC-2 ABC-3 ABC-4 ABC-1" {
		t.Errorf("list = %s; want most recently updated first", got)
	}
	m.renderJira()
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"4 issues · 3 open", "All issues", "✕ Won't Do"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	m.jiraTab.search.SetValue("-is:done")
	m.applyJiraSearch()
	if got := m.allIssuesCounts(); got != "4 issues · 3 open · 3 shown" {
		t.Errorf("searched counts = %q", got)
	}
}

// TestResolvedSearchAndSort: resolution and resolved search, and resolved
// sorts newest first with the unresolved last.
func TestResolvedSearchAndSort(t *testing.T) {
	now := time.Now()
	cards := []jira.Card{
		{Key: "A-1"},
		{Key: "A-2", Resolution: "Duplicate", Resolved: now.Add(-48 * time.Hour)},
		{Key: "A-3", Resolution: "Done", Resolved: now.Add(-10 * 24 * time.Hour)},
	}
	env := jiraQueryEnv{now: now}
	for q, want := range map[string]string{
		"resolution:dup": "A-2", "resolved<7d": "A-2", "resolved>7d": "A-3", "resolution:": "A-1", "-resolution:": "A-2 A-3",
	} {
		var got []string
		for _, c := range cards {
			if jiraCardMatches(c, jiraParseQuery(q), env) {
				got = append(got, c.Key)
			}
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s finds %v, want %s", q, got, want)
		}
	}
	order := []int{0, 1, 2}
	jiraSortResolved.apply(order, cards)
	if fmt.Sprint(order) != "[1 2 0]" {
		t.Errorf("resolved order = %v, want A-2, A-3, then the unresolved", order)
	}
}

// TestFilterBuilderCountsFollowSearch: a value's count is of the cards the
// search's other terms leave; terms on the same field don't narrow it.
func TestFilterBuilderCountsFollowSearch(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.search.SetValue("status:new")
	m.applyJiraSearch()
	m.openFilterBuilder(false)
	labels := func(field string) string {
		var out []string
		for _, it := range m.filterValueItems(field) {
			out = append(out, it.label)
		}
		return strings.Join(out, ", ")
	}
	if got := labels("assignee"); got != "Ada · 1" {
		t.Errorf("assignee values = %q; want only the New cards' assignee", got)
	}
	if got := labels("status"); !strings.Contains(got, "New · 2") || !strings.Contains(got, "In progress · 1") {
		t.Errorf("status values = %q; want every status, the search's own term left out", got)
	}
}

// fakeProjectJira answers a project of n issues, two to a page.
func fakeProjectJira(t *testing.T, n int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/approximate-count":
			fmt.Fprintf(w, `{"count": %d}`, n)
			return
		case "/rest/api/3/search/jql":
		default:
			fmt.Fprint(w, `[]`)
			return
		}
		var body struct {
			Token string `json:"nextPageToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		from := 0
		fmt.Sscan(body.Token, &from)
		var issues []string
		for i := from; i < min(from+2, n); i++ {
			issues = append(issues, fmt.Sprintf(`{"key":"ABC-%d","fields":{"summary":"Issue %d","status":{"id":"1","name":"New"}}}`, i+1, i+1))
		}
		next := ""
		if from+2 < n {
			next = fmt.Sprintf(`,"nextPageToken":"%d"`, from+2)
		}
		fmt.Fprintf(w, `{"issues":[%s]%s}`, strings.Join(issues, ","), next)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAllIssuesReadsProjectWhole: the view on a project never mirrored
// starts a run that reads it page by page into the index, says how far it
// got, and shows the issues once done.
func TestAllIssuesReadsProjectWhole(t *testing.T) {
	srv := fakeProjectJira(t, 5)
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(t.TempDir(), "ix.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	m := New(context.Background(), config.JiraConfig{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", Projects: []string{"ABC"}},
		config.UIConfig{}, nil, "", st).WithIndex(ix)
	m.herdr = nil
	m.width, m.height = 160, 40
	m.resize()
	out, _ := m.handleJiraBoard(jiraBoardMsg{
		seq: m.jiraTab.seq, project: "ABC",
		boards:  []jira.Board{{ID: 1, Name: "ABC board", Type: "scrum"}},
		cfg:     &jira.BoardConfig{Columns: []jira.Column{{Name: "To do", StatusIDs: []string{"1"}}}},
		views:   []jiraView{allIssuesView()},
		viewIdx: 0,
	})
	m = out.(Model)
	if m.jiraTab.mirror == nil {
		t.Fatal("no run started on a project never mirrored")
	}
	// The load's own commands (rules, home) stay unrun: start the run again
	// to have its command alone.
	m.jiraTab.mirror = nil
	queue := runCmd(m.startMirror())
	pages := 0
	for len(queue) > 0 {
		var cmd tea.Cmd
		switch msg := queue[0].(type) {
		case mirrorMsg:
			out, cmd = m.handleMirror(msg)
			m = out.(Model)
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if msg.done > 0 && !msg.finished {
				pages++
				if got := m.allIssuesCounts(); got != fmt.Sprintf("reading all issues: %d of 5", msg.done) {
					t.Errorf("counts mid-run = %q", got)
				}
			}
		case jiraCardsMsg:
			out, _ = m.handleJiraCards(msg)
			m = out.(Model)
		}
		queue = append(queue[1:], runCmd(cmd)...)
	}
	if pages != 2 || !ix.ProjectSync("ABC").Complete || m.jiraTab.mirror != nil {
		t.Fatalf("%d pages shown, sync %+v; want 2 and complete", pages, ix.ProjectSync("ABC"))
	}
	if len(m.jiraTab.cards) != 5 {
		t.Errorf("%d cards shown after the run, want 5", len(m.jiraTab.cards))
	}
}

// runCmd runs cmd and every batch in it, returning their messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, runCmd(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// TestLongListDrawsRowsOnSight: a list of thousands draws the rows near the
// screen only; the wheel and the cursor bring the others in drawn.
func TestLongListDrawsRowsOnSight(t *testing.T) {
	m := bigJiraModel(nil, 3000)
	m.jiraTab.wantLanes = false
	m.buildJiraLanes()
	m.renderJira()
	drawn := 0
	for _, r := range m.jiraTab.rows {
		if r != "" {
			drawn++
		}
	}
	if drawn == 0 || drawn > 3*m.jiraTab.view.Height() {
		t.Errorf("%d of 3000 rows drawn; want about three screens", drawn)
	}
	for range 40 {
		out, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 10, Button: tea.MouseWheelDown})
		m = out.(Model)
	}
	top := m.jiraTab.view.YOffset()
	if top == 0 {
		t.Fatal("the wheel didn't scroll")
	}
	for _, l := range strings.Split(m.jiraTab.view.View(), "\n")[:5] {
		if strings.TrimSpace(ansi.Strip(l)) == "" {
			t.Fatalf("a blank row at offset %d:\n%s", top, m.jiraTab.view.View())
		}
	}
	m.moveJiraCursor(2500)
	if !strings.Contains(ansi.Strip(m.jiraTab.view.View()), m.jiraTab.cards[m.jiraTab.order[m.jiraTab.idx]].Key) {
		t.Error("the cursor's row isn't on screen after a long move")
	}
}
