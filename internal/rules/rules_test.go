package rules

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/jira"
)

func card(key, status, assignee string) jira.Card {
	c := jira.Card{Key: key, Summary: "Fix " + key, Type: "Bug", Status: status, StatusID: status, Priority: "High"}
	if assignee != "" {
		c.Assignee, c.AssigneeID = assignee, "id-"+assignee
	}
	return c
}

func TestDiff(t *testing.T) {
	old := []jira.Card{card("A-1", "To do", ""), card("A-2", "To do", "Ada"), card("A-3", "Done", "")}
	cur := []jira.Card{card("A-1", "Doing", "Ada"), card("A-2", "To do", "Ada"), card("A-4", "To do", "")}
	var got []string
	for _, ev := range Diff(old, cur) {
		got = append(got, Describe(ev))
	}
	want := []string{"A-1 status To do → Doing", "A-1 assignee none → Ada", "A-4 new: Fix A-4"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("diff = %q, want %q", got, want)
	}
}

func compileYAML(t *testing.T, src string) (*Set, []string) {
	t.Helper()
	var rs []Rule
	if err := yaml.Unmarshal([]byte(src), &rs); err != nil {
		t.Fatal(err)
	}
	return Compile(rs)
}

func TestFire(t *testing.T) {
	s, warn := compileYAML(t, `
- name: done-bugs
  on: status
  match: {type: bug, status: [Done, "Won*"], from_status: "In *"}
  actions: [{type: log, text: "{{.Key}} {{.OldStatus}}→{{.Status}}"}]
- name: new-not-mine
  on: [new]
  match: {summary: "(?i)fix", not: {assignee: Ada}}
  actions: [{type: log}]
`)
	if len(warn) != 0 || s.Len() != 2 {
		t.Fatalf("warn = %v, len %d", warn, s.Len())
	}
	done := Event{Kind: Status, Card: card("A-1", "Done", ""), Old: card("A-1", "In review", "")}
	if f := s.Fire(done); len(f) != 1 || f[0].Rule != "done-bugs" || f[0].Text != "A-1 In review→Done" {
		t.Errorf("status fire = %+v", f)
	}
	fromTodo := Event{Kind: Status, Card: card("A-1", "Done", ""), Old: card("A-1", "To do", "")}
	if f := s.Fire(fromTodo); len(f) != 0 {
		t.Errorf("from To do fired %+v", f)
	}
	if f := s.Fire(Event{Kind: New, Card: card("A-2", "To do", "Bob")}); len(f) != 1 || f[0].Text != "A-2 new: Fix A-2" {
		t.Errorf("new fire = %+v", f)
	}
	if f := s.Fire(Event{Kind: New, Card: card("A-2", "To do", "Ada")}); len(f) != 0 {
		t.Errorf("not: fired %+v", f)
	}
	why := s.Explain(Event{Kind: New, Card: card("A-2", "To do", "Ada")})
	if why[0].Text != "on: not new" || why[1].Text != "not: matched" {
		t.Errorf("explain = %+v", why)
	}
}

// TestNestedNot: not.not holds only inside the not.
func TestNestedNot(t *testing.T) {
	s, _ := compileYAML(t, `
- match: {not: {type: Bug, not: {priority: High}}}
  actions: [{type: log}]
`)
	bugHigh := card("A-1", "To do", "")
	bugLow := bugHigh
	bugLow.Priority = "Low"
	story := bugLow
	story.Type = "Story"
	for c, want := range map[*jira.Card]int{&bugHigh: 1, &bugLow: 0, &story: 1} {
		if got := len(s.Fire(Event{Kind: New, Card: *c})); got != want {
			t.Errorf("%s %s fired %d, want %d", c.Type, c.Priority, got, want)
		}
	}
}

func TestCompileWarns(t *testing.T) {
	s, warn := compileYAML(t, `
- {name: a, on: moved, actions: [{type: log}]}
- {name: b, match: {summary: "("}, actions: [{type: log}]}
- {name: c, match: {status: "[" }, actions: [{type: log}]}
- {name: d, actions: [{type: exec}]}
- {name: e}
- {name: f, actions: [{type: log, text: "{{"}]}
- {name: ok, actions: [{type: log}]}
`)
	if s.Len() != 1 || len(warn) != 6 {
		t.Errorf("len %d, warn %q", s.Len(), warn)
	}
}
