package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// planModel is the planning view: two backlog cards beside Sprint 1's two,
// writes going to a fake Jira.
func planModel(t *testing.T, writes *[]string) Model {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*writes = append(*writes, r.Method+" "+r.URL.Path+" "+string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "P"))
	m = out.(Model)
	if m.jiraTab.plan == nil || cmd == nil {
		t.Fatal("P should open planning and load it")
	}
	out, _ = m.handlePlan(planMsg{seq: m.jiraTab.plan.seq,
		left: []jira.Card{{Key: "ABC-7", Summary: "Seven", Points: "3"}, {Key: "ABC-8", Summary: "Eight"}},
		right: []jira.Card{
			{Key: "ABC-1", Summary: "First", Assignee: "Ada", Points: "5"},
			{Key: "ABC-2", Summary: "Second", Points: "2"},
		}})
	return out.(Model)
}

func TestPlanView(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Planning", "Backlog  2 cards · 3p", "Sprint 1  2 cards · 7p", "Ada 5 · — 2", "ABC-7", "ABC-2"} {
		if !strings.Contains(view, want) {
			t.Errorf("planning lacks %q", want)
		}
	}
	out, _ := m.handleJiraKey(keyMsg(t, "esc"))
	if m = out.(Model); m.jiraTab.plan != nil {
		t.Error("esc should bring the board back")
	}
}

// TestPlanMove: space takes a backlog card to the end of the sprint, and
// back to the top of the backlog.
func TestPlanMove(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	p := m.jiraTab.plan
	out, cmd := m.handleJiraKey(keyMsg(t, "space"))
	m = out.(Model)
	if msg := cmd().(planWroteMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(p.sides[0]) != 1 || p.sides[1][2].Key != "ABC-7" {
		t.Fatalf("sides = %v / %v", p.sides[0], p.sides[1])
	}
	if len(writes) != 1 || writes[0] != `POST /rest/agile/1.0/sprint/9/issue {"issues":["ABC-7"]}` {
		t.Errorf("writes = %q", writes)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "l"))
	m = out.(Model)
	_, cmd = m.handleJiraKey(keyMsg(t, "space")) // ABC-1 back
	cmd()
	if p.sides[0][0].Key != "ABC-1" || !strings.HasPrefix(writes[1], "POST /rest/agile/1.0/backlog/issue") {
		t.Errorf("backlog = %v, writes %q", p.sides[0], writes)
	}
}

// TestPlanRank: J moves the card down and ranks it after its neighbour.
func TestPlanRank(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	_, cmd := m.handleJiraKey(keyMsg(t, "J"))
	cmd()
	p := m.jiraTab.plan
	if p.sides[0][1].Key != "ABC-7" || p.idx[0] != 1 {
		t.Fatalf("backlog = %v, idx %d", p.sides[0], p.idx[0])
	}
	if len(writes) != 1 || writes[0] != `PUT /rest/agile/1.0/issue/rank {"issues":["ABC-7"],"rankAfterIssue":"ABC-8"}` {
		t.Errorf("writes = %q", writes)
	}
	if _, cmd := m.handleJiraKey(keyMsg(t, "J")); cmd != nil {
		t.Error("J on the last card should do nothing")
	}
}

func TestPlanCapacity(t *testing.T) {
	cards := []jira.Card{{Assignee: "Ada", Points: "8"}, {Assignee: "Bob", Points: "3"}, {Points: "2"}}
	got := planByAssignee(cards, map[string]float64{"Ada": 5, "default": 10})
	if plain := ansi.Strip(got); plain != "Ada 8/5 · Bob 3/10 · — 2" {
		t.Errorf("plain = %q", plain)
	}
	if !strings.Contains(got, jiraOverStyle.Render("Ada 8/5")) {
		t.Error("over capacity should be marked")
	}
}

// TestPlanMoveMarked: x marks cards on a side; space moves them together.
func TestPlanMoveMarked(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	for range 2 {
		out, _ := m.handleJiraKey(keyMsg(t, "x"))
		m = out.(Model)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "✓ABC-7") {
		t.Error("marks not drawn")
	}
	_, cmd := m.handleJiraKey(keyMsg(t, "space"))
	cmd()
	p := m.jiraTab.plan
	if len(p.sides[0]) != 0 || len(p.sides[1]) != 4 || len(m.jiraTab.marked) != 0 {
		t.Fatalf("sides %v / %v, marked %v", p.sides[0], p.sides[1], m.jiraTab.marked)
	}
	if len(writes) != 1 || !strings.Contains(writes[0], `{"issues":["ABC-7","ABC-8"]}`) {
		t.Errorf("writes = %q", writes)
	}
}
