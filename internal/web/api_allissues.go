package web

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/mirror"
	"github.com/cornedor/laneway/internal/ui"
)

// The all issues view (the TUI's, internal/ui/allissues.go): every issue of
// a project from its mirror in the site's index. The first time, a run in
// the background reads the project whole; until it is done every answer
// carries how far it got (Sync), and the page asks again.

// projectKeyRe is what a Jira project key looks like.
var projectKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// mirrorRuns are the runs under way, by site and project; err keeps how the
// last one of a project failed, for its next answer.
type mirrorRuns struct {
	mu   sync.Mutex
	runs map[string]*mirrorRun
}

type mirrorRun struct {
	done, total int
	err         error
}

// syncOut is a run's progress in an answer.
type syncOut struct {
	Done, Total int
}

// allIssuesCards: GET /boards/{board}/cards?allissues=1&project=ABC, jql=
// the quick filters. {cards, total, sync?, note?}.
func allIssuesCards(ctx context.Context, s *Server, r *http.Request) (any, error) {
	ix := s.opt.Index
	if ix == nil {
		return nil, badRequest(i18n.T("this site keeps no issue index"))
	}
	project := Q(r, "project")
	if !projectKeyRe.MatchString(project) {
		return nil, badRequest(i18n.T("bad project key"))
	}
	cards, note, err := ui.AllIssues(ctx, s.Client(), ix, project, Q(r, "jql"))
	if err != nil {
		return nil, err
	}
	observeRules(s, r, cards)
	out := map[string]any{"cards": cards, "total": len(cards)}
	if note != "" {
		out["note"] = note
	}
	sync, err := s.mirrorProject(project, ix)
	if err != nil {
		return nil, err
	}
	if sync != nil {
		out["sync"] = sync
	}
	return out, nil
}

// mirrorProject is how far the run reading project is, starting one when
// its mirror was never completed; nil once it is. A failed run's error comes
// back once, and the next ask starts again.
func (s *Server) mirrorProject(project string, ix *index.Index) (*syncOut, error) {
	if ix.ProjectSync(project).Complete {
		return nil, nil
	}
	m := &s.sites.mirrors
	k := s.opt.Site + "\x1f" + project
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs == nil {
		m.runs = map[string]*mirrorRun{}
	}
	run := m.runs[k]
	switch {
	case run != nil && run.err != nil:
		delete(m.runs, k)
		return nil, run.err
	case run != nil:
		return &syncOut{Done: run.done, Total: run.total}, nil
	}
	run = &mirrorRun{}
	m.runs[k] = run
	go s.runMirror(k, run, project, ix)
	return &syncOut{}, nil
}

// runMirror reads project whole, a page at a time, keeping run's progress.
func (s *Server) runMirror(k string, run *mirrorRun, project string, ix *index.Index) {
	m, c := &s.sites.mirrors, s.Client()
	step := func(f func(ctx context.Context) error) error {
		ctx, cancel := context.WithTimeout(s.ctx, c.Scaled(60*time.Second))
		defer cancel()
		return f(ctx)
	}
	var r *mirror.Run
	err := step(func(ctx context.Context) (err error) {
		r, err = mirror.Start(ctx, c, ix, project)
		return err
	})
	for err == nil && !r.Finished() {
		m.mu.Lock()
		run.done, run.total = r.Done, r.Total
		m.mu.Unlock()
		err = step(func(ctx context.Context) error { return r.Next(ctx, c, ix) })
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		run.err = err
		return
	}
	delete(m.runs, k)
}
