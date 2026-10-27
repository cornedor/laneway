package people

import (
	"context"
	"encoding/json"
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

// Picker ranks one site's person pickers. A picker never waits on the
// forge: Warm reads an issue's merge requests in the background, when the
// issue opens, and keeps the result in Store; a picker opened before it is
// done ranks by the picks alone.
type Picker struct {
	Rules []Rule
	Known map[string]string // jira.git_people
	Store Store
	Forge Finder // nil without one
}

// warmFor is how long an issue's merge requests' suggestions are kept.
const warmFor = 24 * time.Hour

// warmPrefix keys an issue's suggestions in the Store.
const warmPrefix = "people.mr."

type warmed struct {
	At      int64                         `json:"t"` // unix seconds
	Sources []string                      `json:"s"`
	By      map[string]map[string]float64 `json:"b"` // source → account → score
}

// Sources ranks field (its id and name) in project.
func (p Picker) Sources(project, id, name string) []string {
	return For(p.Rules, project, id, name)
}

// remote are the forge's sources some rule asks for in project, in
// Sources' order: what Warm reads, whichever field is picked.
func (p Picker) remote(project string) []string {
	var want []string
	for _, r := range p.Rules {
		if len(r.Projects) > 0 && !slices.ContainsFunc(r.Projects, func(x string) bool { return strings.EqualFold(x, project) }) {
			continue
		}
		want = append(want, r.Suggest...)
	}
	var out []string
	for _, s := range Sources {
		if s != Frequent && slices.Contains(want, s) {
			out = append(out, s)
		}
	}
	return out
}

// Ranks ranks field for key (an issue or a project key) by sources: its
// picks, and what Warm kept of key's merge requests. Never slow.
func (p Picker) Ranks(key string, sources []string, field string) map[string]Rank {
	by := map[string]map[string]float64{}
	if slices.Contains(sources, Frequent) {
		by[Frequent] = Picked(p.Store, Project(key), field, time.Now())
	}
	if w, ok := p.ready(key); ok {
		for _, s := range sources {
			if m, ok := w.By[s]; ok {
				by[s] = m
			}
		}
	}
	return Combine(sources, by)
}

func (p Picker) ready(key string) (warmed, bool) {
	var w warmed
	if p.Store == nil {
		return w, false
	}
	v, ok, err := p.Store.GetMeta(warmPrefix + key)
	if err != nil || !ok || json.Unmarshal([]byte(v), &w) != nil {
		return w, false
	}
	return w, time.Since(time.Unix(w.At, 0)) < warmFor
}

// running are the issues being warmed, so a second open starts none;
// warming bounds how many run at once (a board prefetching issues).
var (
	running sync.Map
	warming = make(chan struct{}, 2)
)

// Wants is whether Warm has anything to do for key: a rule asks for its
// merge requests and none are kept yet.
func (p Picker) Wants(key string) bool {
	if p.Forge == nil || p.Store == nil || !strings.Contains(key, "-") {
		return false
	}
	src := p.remote(Project(key))
	if len(src) == 0 {
		return false
	}
	w, ok := p.ready(key)
	return !ok || !slices.Equal(w.Sources, src)
}

// Warm reads key's merge requests for the sources the project's rules ask
// for, finds their people in Jira and keeps them for a day. Call it in the
// background when an issue opens; it does nothing when Wants is false,
// another Warm of key runs, or two others do.
func (p Picker) Warm(ctx context.Context, key string, lookup Lookup) {
	if !p.Wants(key) {
		return
	}
	if _, busy := running.LoadOrStore(key, true); busy {
		return
	}
	defer running.Delete(key)
	select {
	case warming <- struct{}{}:
		defer func() { <-warming }()
	default:
		return // enough at once: the next open tries again
	}
	src := p.remote(Project(key))
	by := Match(ctx, p.Forge.Candidates(ctx, key, src), lookup, p.Known, p.Store)
	if ctx.Err() != nil {
		return // cut short: not kept
	}
	b, _ := json.Marshal(warmed{At: time.Now().Unix(), Sources: src, By: by})
	_ = p.Store.SetMeta(warmPrefix+key, string(b))
	p.prune()
}

// prune forgets the issues' suggestions past warmFor, where the store can.
func (p Picker) prune() {
	st, ok := p.Store.(interface {
		Prefixed(prefix string) map[string]string
		DeleteMeta(key string) error
	})
	if !ok {
		return
	}
	for k, v := range st.Prefixed(warmPrefix) {
		var w warmed
		if json.Unmarshal([]byte(v), &w) != nil || time.Since(time.Unix(w.At, 0)) >= warmFor {
			_ = st.DeleteMeta(k)
		}
	}
}
