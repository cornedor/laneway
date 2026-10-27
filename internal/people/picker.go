package people

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/forge"
)

// Finder finds the candidates the merge requests naming key suggest
// (gitlab.Sites).
type Finder interface {
	Candidates(ctx context.Context, key string, sources []string) []forge.Candidate
}

// Picker ranks one site's person pickers.
type Picker struct {
	Rules []Rule
	Known map[string]string // jira.git_people
	Store Store
	Forge Finder // nil without one
	Memo  *Memo  // keeps the forge's answers a while; nil asks every time
}

// memoFor is how long the forge's candidates for an issue are kept.
const memoFor = 10 * time.Minute

// Memo keeps the forge's candidates by issue and sources. Safe for
// concurrent use.
type Memo struct {
	mu sync.Mutex
	m  map[string]memoEntry
}

type memoEntry struct {
	cands []forge.Candidate
	at    time.Time
}

func (p Picker) candidates(ctx context.Context, key string, sources []string) []forge.Candidate {
	if p.Memo == nil {
		return p.Forge.Candidates(ctx, key, sources)
	}
	k := key + " " + strings.Join(sources, ",")
	p.Memo.mu.Lock()
	e, ok := p.Memo.m[k]
	p.Memo.mu.Unlock()
	if ok && time.Since(e.at) < memoFor {
		return e.cands
	}
	cands := p.Forge.Candidates(ctx, key, sources)
	if ctx.Err() != nil {
		return cands // cut short: not kept
	}
	p.Memo.mu.Lock()
	if p.Memo.m == nil {
		p.Memo.m = map[string]memoEntry{}
	}
	p.Memo.m[k] = memoEntry{cands, time.Now()}
	p.Memo.mu.Unlock()
	return cands
}

// Sources ranks field (its id and name) in project.
func (p Picker) Sources(project, id, name string) []string {
	return For(p.Rules, project, id, name)
}

// Local ranks field in project by what needs no network: its picks.
func (p Picker) Local(sources []string, project, field string) map[string]Rank {
	return Combine(sources, p.local(sources, project, field))
}

func (p Picker) local(sources []string, project, field string) map[string]map[string]float64 {
	by := map[string]map[string]float64{}
	if slices.Contains(sources, Frequent) {
		by[Frequent] = Picked(p.Store, project, field, time.Now())
	}
	return by
}

// Ranks ranks field for key by every source, the forge's included: what
// Local gives once the merge requests are read.
func (p Picker) Ranks(ctx context.Context, key string, sources []string, field string, lookup Lookup) map[string]Rank {
	by := p.local(sources, Project(key), field)
	if p.Forge != nil && Remote(sources) && strings.Contains(key, "-") { // an issue, not a new one's project
		var remote []string
		for _, s := range sources {
			if s != Frequent {
				remote = append(remote, s)
			}
		}
		for src, m := range Match(ctx, p.candidates(ctx, key, remote), lookup, p.Known, p.Store) {
			by[src] = m
		}
	}
	return Combine(sources, by)
}
