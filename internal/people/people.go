// Package people ranks the people a person picker offers. Each picker (a
// project's assignee, reporter or user field) remembers who was picked in it,
// recent picks weighing more; a field's rule can add what the issue's merge
// requests say (internal/forge's Candidates): its code owners, who reviewed
// those files before, who wrote the lines it changes.
//
// The sources a rule lists rank in their order: the first weighs most, and
// someone several name rises above someone one names.
package people

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// The sources a rule can list. Frequent is local; the others read the
// forge.
const (
	Frequent   = "frequent"
	CodeOwners = "codeowners"
	Reviewers  = "reviewers"
	Blame      = "blame"
)

// Sources are the names a rule takes, in their default order.
var Sources = []string{CodeOwners, Reviewers, Blame, Frequent}

// Rule is how one person field ranks: jira.pickers in the config. Field is
// the field's name or id ("Code Reviewer", "customfield_10042", "assignee");
// Projects narrows it, none for all; Suggest are the sources, an empty
// list none.
type Rule struct {
	Field    string   `yaml:"field"`
	Projects []string `yaml:"projects,omitempty"`
	Suggest  []string `yaml:"suggest"`
}

// Check names what is wrong with rules, for the config's warnings.
func Check(rules []Rule) []string {
	var out []string
	for i, r := range rules {
		if strings.TrimSpace(r.Field) == "" {
			out = append(out, fmt.Sprintf("jira.pickers[%d]: field is required", i))
		}
		for _, s := range r.Suggest {
			if !slices.Contains(Sources, s) {
				out = append(out, fmt.Sprintf("jira.pickers[%d]: unknown source %q (one of %s)", i, s, strings.Join(Sources, ", ")))
			}
		}
	}
	return out
}

// For is the sources ranking field (id and name) in project: the first rule
// naming it, else frequent alone.
func For(rules []Rule, project, id, name string) []string {
	for _, r := range rules {
		f := strings.TrimSpace(r.Field)
		if !strings.EqualFold(f, id) && !strings.EqualFold(f, name) {
			continue
		}
		if len(r.Projects) > 0 && !slices.ContainsFunc(r.Projects, func(p string) bool { return strings.EqualFold(p, project) }) {
			continue
		}
		return slices.DeleteFunc(slices.Clone(r.Suggest), func(s string) bool { return !slices.Contains(Sources, s) })
	}
	return []string{Frequent}
}

// Remote is whether sources reads the forge.
func Remote(sources []string) bool {
	return slices.ContainsFunc(sources, func(s string) bool { return s != Frequent })
}

// Project is the project of key, an issue key or a project key.
func Project(key string) string {
	p, _, _ := strings.Cut(key, "-")
	return strings.ToUpper(p)
}

// Store is where picks are kept: the site's state.
type Store interface {
	GetMeta(key string) (string, bool, error)
	SetMeta(key, value string) error
}

// halfLife is how long a pick takes to count half.
const halfLife = 30 * 24 * time.Hour

// keep is how many people a picker remembers.
const keep = 30

type pick struct {
	Score float64 `json:"s"`
	At    int64   `json:"t"` // unix seconds
}

func metaKey(project, field string) string {
	return "picks." + strings.ToUpper(project) + "." + strings.ToLower(field)
}

func decayed(p pick, now time.Time) float64 {
	age := now.Sub(time.Unix(p.At, 0))
	return p.Score * math.Pow(0.5, float64(age)/float64(halfLife))
}

func load(st Store, project, field string) map[string]pick {
	m := map[string]pick{}
	if st == nil {
		return m
	}
	if v, ok, err := st.GetMeta(metaKey(project, field)); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &m)
	}
	return m
}

// Record counts a pick of account in project's field.
func Record(st Store, project, field, account string, now time.Time) error {
	if st == nil || account == "" || project == "" || field == "" {
		return nil
	}
	m := load(st, project, field)
	m[account] = pick{Score: decayed(m[account], now) + 1, At: now.Unix()}
	if len(m) > keep {
		ids := make([]string, 0, len(m))
		for id := range m {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return decayed(m[ids[i]], now) > decayed(m[ids[j]], now) })
		for _, id := range ids[keep:] {
			delete(m, id)
		}
	}
	b, _ := json.Marshal(m)
	return st.SetMeta(metaKey(project, field), string(b))
}

// Picked is how much each account was picked in project's field, now.
func Picked(st Store, project, field string, now time.Time) map[string]float64 {
	out := map[string]float64{}
	for id, p := range load(st, project, field) {
		out[id] = decayed(p, now)
	}
	return out
}

// Rank is one account's place: its score and the source that weighed most
// in it.
type Rank struct {
	Score float64
	Why   string
}

// Combine ranks the accounts by source (source → account → score) the way
// sources orders them: each source's scores are scaled to its best one, and
// the first source weighs len(sources), the last 1.
func Combine(sources []string, by map[string]map[string]float64) map[string]Rank {
	out := map[string]Rank{}
	best := map[string]float64{} // account → the largest part, for Why
	for i, src := range sources {
		w := float64(len(sources) - i)
		var top float64
		for _, s := range by[src] {
			top = max(top, s)
		}
		if top <= 0 {
			continue
		}
		for id, s := range by[src] {
			if s <= 0 {
				continue
			}
			part := w * s / top
			r := out[id]
			r.Score += part
			if part > best[id] {
				best[id], r.Why = part, src
			}
			out[id] = r
		}
	}
	return out
}

// Order sorts xs by rank, best first; the unranked keep their order after.
func Order[T any](xs []T, id func(T) string, ranks map[string]Rank) {
	sort.SliceStable(xs, func(i, j int) bool { return ranks[id(xs[i])].Score > ranks[id(xs[j])].Score })
}
