package rules

import (
	"strings"
	"testing"
	"time"

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
- {name: g, actions: [{type: highlight, color: red}]}
- {name: ok, actions: [{type: log}, {type: highlight, color: "#f00"}]}
`)
	if s.Len() != 1 || len(warn) != 7 {
		t.Errorf("len %d, warn %q", s.Len(), warn)
	}
}

func TestByMe(t *testing.T) {
	s, _ := compileYAML(t, `
- match: {by_me: false}
  actions: [{type: log}]
`)
	if !s.UsesByMe() {
		t.Fatal("UsesByMe = false")
	}
	yes, no := true, false
	ev := Event{Kind: New, Card: card("A-1", "To do", "")}
	for _, c := range []struct {
		by   *bool
		want int
	}{{nil, 0}, {&yes, 0}, {&no, 1}} {
		ev.ByMe = c.by
		if got := len(s.Fire(ev)); got != c.want {
			t.Errorf("by_me %v fired %d, want %d", c.by, got, c.want)
		}
	}
	if plain, _ := compileYAML(t, "- actions: [{type: log}]\n"); plain.UsesByMe() {
		t.Error("UsesByMe without by_me")
	}
}

// TestWatch: a watch rule fires only on its own search's changes, a board
// rule only on a board's; watches merge by JQL at the shortest every.
func TestWatch(t *testing.T) {
	s, warn := compileYAML(t, `
- name: board
  actions: [{type: log}]
- name: mine
  watch: assignee = currentUser()
  every: 10m
  actions: [{type: log}]
- name: mine-fast
  watch: assignee = currentUser()
  every: 2m
  actions: [{type: log}]
- name: other
  watch: project = X
  actions: [{type: log}]
- {name: fast, watch: a = b, every: 30s, actions: [{type: log}]}
- {name: lonely, every: 5m, actions: [{type: log}]}
`)
	if len(warn) != 2 || !strings.Contains(warn[0], "under 1m") || !strings.Contains(warn[1], "needs a watch") {
		t.Errorf("warn = %q", warn)
	}
	fired := func(watch string) (names []string) {
		for _, f := range s.Fire(Event{Kind: New, Card: card("A-1", "To do", ""), Watch: watch}) {
			names = append(names, f.Rule)
		}
		return names
	}
	if got := fired(""); strings.Join(got, ",") != "board" {
		t.Errorf("board fired %q", got)
	}
	if got := fired("assignee = currentUser()"); strings.Join(got, ",") != "mine,mine-fast" {
		t.Errorf("watch fired %q", got)
	}
	ws := s.Watches()
	if len(ws) != 2 || ws[0].Every != 2*time.Minute || ws[1].JQL != "project = X" || ws[1].Every != DefaultEvery {
		t.Errorf("watches = %+v", ws)
	}
}
