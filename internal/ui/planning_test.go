package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// TestPlanCloseSprint: C twice moves the unfinished issues on, then closes;
// a done one stays.
func TestPlanCloseSprint(t *testing.T) {
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			io.WriteString(w, `[]`)
			return
		}
		if r.Method == http.MethodGet {
			if !strings.Contains(r.URL.Query().Get("jql"), "status not in (5,6)") {
				t.Errorf("jql = %q, want only the unfinished", r.URL.Query().Get("jql"))
			}
			if len(writes) > 0 { // moved: none left
				io.WriteString(w, `{"total":0,"issues":[]}`)
				return
			}
			io.WriteString(w, `{"total":1,"issues":[{"key":"ABC-1","fields":{"status":{"id":"1"}}}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	var ignored []string
	m := planModel(t, &ignored)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "S"))
	if m = out.(Model); cmd != nil || m.jiraFieldName != "plan-end" {
		t.Errorf("S on the active sprint should ask its new end, got %q", m.jiraFieldName)
	}
	m.closeJiraField()
	out, cmd = m.handleJiraKey(keyMsg(t, "C"))
	m = out.(Model)
	if cmd != nil || !strings.Contains(m.status, "C again completes Sprint 1, unfinished issues to the backlog") {
		t.Fatalf("first C: %q", m.status)
	}
	_, cmd = m.handleJiraKey(keyMsg(t, "C"))
	msg := cmd().(planSprintMsg)
	if msg.err != nil || !strings.Contains(msg.what, "1 unfinished") {
		t.Fatalf("%+v", msg)
	}
	if len(writes) != 2 || writes[0] != `POST /rest/agile/1.0/backlog/issue {"issues":["ABC-1"]}` || writes[1] != `POST /rest/agile/1.0/sprint/9 {"state":"closed"}` {
		t.Errorf("writes = %q", writes)
	}
}

// TestPlanStartSprint: S on a future sprint asks the end, then starts it.
func TestPlanStartSprint(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	p := m.jiraTab.plan
	p.sprints = append(p.sprints, jiraView{kind: jiraViewSprint, name: "Sprint 2", sprint: 10})
	p.target = 1
	out, _ := m.handleJiraKey(keyMsg(t, "S"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "plan-start" || m.jiraFieldInput.Value() != "+2w" {
		t.Fatalf("input %q %q", m.jiraFieldName, m.jiraFieldInput.Value())
	}
	_, cmd := m.applyJiraField()
	if msg := cmd().(planSprintMsg); !strings.Contains(msg.what, "Sprint 2 started") {
		t.Errorf("%+v", msg)
	}
	if len(writes) != 1 || !strings.Contains(writes[0], "/rest/agile/1.0/sprint/10") || !strings.Contains(writes[0], `"state":"active"`) {
		t.Errorf("writes = %q", writes)
	}
}

// TestPlanNewSprint: N asks the name, numbered on from the last sprint,
// and creates it on the board.
func TestPlanNewSprint(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	out, _ := m.handleJiraKey(keyMsg(t, "N"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "plan-new" || m.jiraFieldInput.Value() != "Sprint 2" {
		t.Fatalf("input %q %q", m.jiraFieldName, m.jiraFieldInput.Value())
	}
	_, cmd := m.applyJiraField()
	if msg := cmd().(planSprintMsg); msg.err != nil || msg.what != "Sprint 2 created" {
		t.Fatalf("%+v", msg)
	}
	if len(writes) != 1 || writes[0] != `POST /rest/agile/1.0/sprint {"name":"Sprint 2","originBoardId":1}` {
		t.Errorf("writes = %q", writes)
	}
	if got := nextSprintName([]jiraView{{name: "Planning"}}); got != "" {
		t.Errorf("no number: %q", got)
	}
}

// TestPlanGoal: E edits the target sprint's goal.
func TestPlanGoal(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	m.jiraTab.plan.sprints[0].goal = "Ship login"
	out, _ := m.handleJiraKey(keyMsg(t, "E"))
	m = out.(Model)
	if m.jiraFieldName != "plan-goal" || m.jiraFieldInput.Value() != "Ship login" {
		t.Fatalf("input %q %q", m.jiraFieldName, m.jiraFieldInput.Value())
	}
	m.jiraFieldInput.SetValue("Ship login and search")
	_, cmd := m.applyJiraField()
	cmd()
	if len(writes) != 1 || writes[0] != `POST /rest/agile/1.0/sprint/9 {"goal":"Ship login and search"}` {
		t.Errorf("writes = %q", writes)
	}
}

// TestPlanCloseConfirmResets: any other key between the two Cs cancels.
func TestPlanCloseConfirmResets(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	out, _ := m.handleJiraKey(keyMsg(t, "C"))
	m = out.(Model)
	out, _ = m.handleJiraKey(keyMsg(t, "j"))
	m = out.(Model)
	if _, cmd := m.handleJiraKey(keyMsg(t, "C")); cmd != nil {
		t.Error("C after another key should ask again, not complete")
	}
}

// TestPlanRenameAndEnd: R renames the sprint, S on the active one moves its
// end.
func TestPlanRenameAndEnd(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	out, _ := m.handleJiraKey(keyMsg(t, "R"))
	m = out.(Model)
	if m.jiraFieldName != "plan-rename" || m.jiraFieldInput.Value() != "Sprint 1" {
		t.Fatalf("rename input %q %q", m.jiraFieldName, m.jiraFieldInput.Value())
	}
	m.jiraFieldInput.SetValue("Sprint 1: login")
	_, cmd := m.applyJiraField()
	cmd()
	out, _ = m.handleJiraKey(keyMsg(t, "S"))
	m = out.(Model)
	m.jiraFieldInput.SetValue("2026-10-09")
	_, cmd = m.applyJiraField()
	cmd()
	if len(writes) != 2 || writes[0] != `POST /rest/agile/1.0/sprint/9 {"name":"Sprint 1: login"}` || !strings.Contains(writes[1], `"endDate":"2026-10-09T17:00:00`) {
		t.Errorf("writes = %q", writes)
	}
}

// TestPlanDrag: a card dragged onto the other side moves there alone when
// it isn't marked; while held it is a ghost and the target says drop.
func TestPlanDrag(t *testing.T) {
	var writes []string
	m := planModel(t, &writes)
	m.jiraTab.marked = map[string]bool{"ABC-7": true}
	y := jiraBodyTop + 3 // the head, the per-assignee line, then ABC-7, ABC-8
	out, _ := m.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	p := m.jiraTab.plan
	if p.drag.key != "ABC-8" {
		t.Fatalf("armed on %q", p.drag.key)
	}
	right := m.jiraTab.view.Width() - 5
	out, _ = m.Update(tea.MouseMotionMsg{X: right, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "◂ drop") || !strings.Contains(view, "┊ ABC-8") {
		t.Fatalf("no drag feedback:\n%s", view)
	}
	out, cmd := m.Update(tea.MouseReleaseMsg{X: right, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the drop should write")
	}
	cmd()
	if len(p.sides[0]) != 1 || p.sides[1][2].Key != "ABC-8" || p.drag.key != "" {
		t.Fatalf("sides %v / %v", p.sides[0], p.sides[1])
	}
	if len(writes) != 1 || writes[0] != `POST /rest/agile/1.0/sprint/9/issue {"issues":["ABC-8"]}` {
		t.Errorf("writes = %q", writes)
	}
}
