package people

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/jira"
)

type memStore map[string]string

func (m memStore) GetMeta(k string) (string, bool, error) { v, ok := m[k]; return v, ok, nil }
func (m memStore) SetMeta(k, v string) error              { m[k] = v; return nil }

// The first rule naming the field (by id or name) in the project counts;
// none ranks by picks.
func TestFor(t *testing.T) {
	rules := []Rule{
		{Field: "Tester", Projects: []string{"ABC"}, Suggest: []string{Blame}},
		{Field: "customfield_1", Suggest: []string{CodeOwners, Frequent, "bogus"}},
		{Field: "reporter", Suggest: []string{}},
	}
	for _, c := range []struct {
		project, id, name string
		want              []string
	}{
		{"ABC", "customfield_9", "tester", []string{Blame}},
		{"XYZ", "customfield_9", "Tester", []string{Frequent}},
		{"XYZ", "customfield_1", "Code Reviewer", []string{CodeOwners, Frequent}},
		{"ABC", "reporter", "Reporter", []string{}},
		{"ABC", "assignee", "Assignee", []string{Frequent}},
	} {
		if got := For(rules, c.project, c.id, c.name); !slices.Equal(got, c.want) {
			t.Errorf("For(%s, %s) = %v, want %v", c.project, c.name, got, c.want)
		}
	}
	if w := Check(rules); len(w) != 1 || !strings.Contains(w[0], "bogus") {
		t.Errorf("Check = %v", w)
	}
}

// A pick counts half after the half-life; the picker remembers keep people.
func TestRecord(t *testing.T) {
	st := memStore{}
	now := time.Unix(1_700_000_000, 0)
	Record(st, "abc", "Tester", "ada", now.Add(-halfLife))
	Record(st, "ABC", "tester", "bo", now)
	Record(st, "ABC", "tester", "bo", now)
	got := Picked(st, "ABC", "tester", now)
	if d := got["ada"] - 0.5; d > 1e-9 || d < -1e-9 || got["bo"] != 2 {
		t.Errorf("Picked = %v", got)
	}
	if len(Picked(st, "ABC", "assignee", now)) != 0 {
		t.Error("another field shares the picks")
	}
	for i := range keep + 5 {
		Record(st, "ABC", "tester", fmt.Sprint("p", i), now)
	}
	if got := Picked(st, "ABC", "tester", now); len(got) != keep || got["bo"] != 2 {
		t.Errorf("kept %d, bo %v", len(got), got["bo"])
	}
}

// The first source weighs most, each scaled to its best; Why is the
// biggest part; Order keeps the unranked in their order.
func TestCombineOrder(t *testing.T) {
	ranks := Combine([]string{CodeOwners, Frequent}, map[string]map[string]float64{
		CodeOwners: {"ada": 2, "cy": 1},
		Frequent:   {"bo": 10, "cy": 10},
	})
	if ranks["cy"].Score != 2 || ranks["cy"].Why != CodeOwners || ranks["ada"].Score != 2 || ranks["bo"].Why != Frequent {
		t.Errorf("ranks = %v", ranks)
	}
	xs := []string{"x", "bo", "y", "ada", "cy"}
	Order(xs, func(s string) string { return s }, ranks)
	if !slices.Equal(xs, []string{"ada", "cy", "bo", "x", "y"}) {
		t.Errorf("Order = %v", xs)
	}
}

// Candidates are found by git_people, a remembered match, email, then a
// name only one person has.
func TestMatch(t *testing.T) {
	jiraUsers := []jira.User{{AccountID: "id-ada", DisplayName: "Ada Lovelace"}, {AccountID: "id-bo", DisplayName: "Bo"}, {AccountID: "id-bo2", DisplayName: "Bo"}, {AccountID: "id-cy", DisplayName: "Cy Young"}}
	var asked []string
	lookup := func(_ context.Context, q string) ([]jira.User, error) {
		asked = append(asked, q)
		if strings.EqualFold(q, "cy@example.com") {
			return jiraUsers[3:], nil
		}
		var out []jira.User
		for _, u := range jiraUsers {
			if strings.Contains(strings.ToLower(u.DisplayName), strings.ToLower(q)) {
				out = append(out, u)
			}
		}
		return out, nil
	}
	st := memStore{}
	cands := []forge.Candidate{
		{Username: "ada", Name: "A. L.", Scores: map[string]float64{Reviewers: 3}},
		{Name: "Bo", Scores: map[string]float64{Blame: 2}}, // two Bos: left out
		{Name: "Cy", Email: "Cy@example.com", Scores: map[string]float64{Blame: 1}},
		{Username: "dee", Scores: map[string]float64{CodeOwners: 1}},
	}
	known := map[string]string{"ADA": "Ada Lovelace", "dee": "712020:0123456789abcdef"}
	got := Match(context.Background(), cands, lookup, known, st)
	want := map[string]map[string]float64{Reviewers: {"id-ada": 3}, Blame: {"id-cy": 1}, CodeOwners: {"712020:0123456789abcdef": 1}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Match = %v, want %v", got, want)
	}
	if st["people.git.cy@example.com"] != "id-cy" {
		t.Errorf("not remembered: %v", st)
	}
	asked = nil
	Match(context.Background(), cands[2:3], lookup, nil, st)
	if len(asked) != 0 {
		t.Errorf("remembered match asked Jira: %v", asked)
	}
}

type finder []forge.Candidate

func (f *finder) Candidates(context.Context, string, []string) []forge.Candidate {
	c := *f
	*f = nil // a second call finds nothing: the memo must answer
	return c
}

// Ranks adds the forge's part to the picks, kept in the memo; a project
// key (a new issue) never asks the forge.
func TestRanks(t *testing.T) {
	st := memStore{}
	Record(st, "ABC", "customfield_1", "id-bo", time.Now())
	f := &finder{{Username: "ada", Name: "Ada", Scores: map[string]float64{Blame: 5}}}
	p := Picker{Rules: []Rule{{Field: "Code Reviewer", Suggest: []string{Blame, Frequent}}}, Store: st, Forge: f, Memo: &Memo{}}
	lookup := func(_ context.Context, q string) ([]jira.User, error) {
		return []jira.User{{AccountID: "id-ada", DisplayName: "Ada"}}, nil
	}
	src := p.Sources("ABC", "customfield_1", "Code Reviewer")
	for range 2 {
		r := p.Ranks(context.Background(), "ABC-1", src, "customfield_1", lookup)
		if r["id-ada"].Why != Blame || r["id-ada"].Score <= r["id-bo"].Score || r["id-bo"].Why != Frequent {
			t.Fatalf("Ranks = %v", r)
		}
	}
	*f = finder{{Username: "x", Scores: map[string]float64{Blame: 1}}}
	if r := p.Ranks(context.Background(), "ABC", src, "customfield_1", lookup); len(r) != 1 {
		t.Errorf("a project key asked the forge: %v", r)
	}
}
