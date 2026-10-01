package web

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

func init() {
	get("/session", session)
	get("/projects", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.Client().ListProjects(ctx)
	})
	get("/projects/{project}/boards", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.Client().Boards(ctx, r.PathValue("project"))
	})
	get("/boards/{board}", boardBundle)
	get("/boards/{board}/cards", boardCards)
	get("/issues/{key}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key := r.PathValue("key")
		if !jira.ValidKey(key) {
			return nil, badRequest("bad issue key")
		}
		if Q(r, "fresh") != "" {
			s.Client().Invalidate(key)
		}
		return s.Client().Get(ctx, key)
	})
	get("/issues/{key}/transitions", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.Client().Transitions(ctx, r.PathValue("key"))
	})
	post("/issues/{key}/transition", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ ID string }](r)
		if err != nil {
			return nil, err
		}
		key := r.PathValue("key")
		err = s.Client().DoTransition(ctx, key, b.ID)
		s.Client().Invalidate(key)
		return nil, err
	})
	post("/issues/{key}/rank", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct {
			Other string
			After bool
		}](r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().Rank(ctx, r.PathValue("key"), b.Other, b.After)
	})
	get("/prefs", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		out := map[string]string{}
		for _, k := range s.opt.Store.Prefixed("web:") {
			if v, ok, _ := s.opt.Store.GetMeta(k); ok {
				out[k[len("web:"):]] = v
			}
		}
		return out, nil
	})
	put("/prefs/{name}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Value string }](r)
		if err != nil {
			return nil, err
		}
		return nil, s.opt.Store.SetMeta("web:"+r.PathValue("name"), b.Value)
	})
}

// session is what the frontend needs at start: who, where, defaults.
func session(ctx context.Context, s *Server, r *http.Request) (any, error) {
	if s.opt.Setup != nil {
		return setupSession(s), nil
	}
	o := s.opt
	me, _ := s.Client().Myself(ctx)
	return map[string]any{
		"site": o.Site, "sites": o.Sites, "defaultName": o.DefaultName, "demo": o.Demo, "version": o.Version,
		"baseURL": s.Client().BaseURL(), "me": me,
		"projects": o.Jira.Projects, "ui": s.UIConfig(), "autostart": s.autostartInfo(), "starred": starred(s),
		"repos":   slices.Sorted(maps.Keys(o.Jira.Repos)), // projects with a checkout: draft PR, removing a worktree
		"addSite": addSiteInfo(s),
	}, nil
}

func boardID(r *http.Request) (int, error) {
	id, err := strconv.Atoi(r.PathValue("board"))
	if err != nil {
		return 0, badRequest("bad board id")
	}
	return id, nil
}

// boardBundle is everything a board view needs besides its cards: columns,
// quick filters, sprints (open and future; closed with ?closed=1).
func boardBundle(ctx context.Context, s *Server, r *http.Request) (any, error) {
	id, err := boardID(r)
	if err != nil {
		return nil, err
	}
	c := s.Client()
	cfg, err := c.BoardConfiguration(ctx, id)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"id": id, "config": cfg}
	qf, _ := c.QuickFilters(ctx, id)
	out["quickFilters"] = append(localQuick(s.UIConfig().QuickFilters), qf...)
	if sp, err := c.Sprints(ctx, id); err == nil {
		out["sprints"] = sp
	}
	if Q(r, "closed") != "" {
		if sp, err := c.ClosedSprints(ctx, id); err == nil {
			out["closedSprints"] = sp
		}
	}
	if names, err := c.StatusNames(ctx); err == nil {
		out["statusNames"] = names
	}
	return out, nil
}

// localQuick are ui.quick_filters, shown before every board's own, as the
// TUI's (ids -1, -2, …); one without a name or JQL is left out.
func localQuick(qs []config.QuickFilter) []jira.QuickFilter {
	out := []jira.QuickFilter{}
	for _, q := range qs {
		if strings.TrimSpace(q.Name) != "" && strings.TrimSpace(q.JQL) != "" {
			out = append(out, jira.QuickFilter{ID: -1 - len(out), Name: q.Name, JQL: q.JQL})
		}
	}
	return out
}

// boardCards: ?sprint=ID → that sprint, ?backlog=1 → backlog, ?kanban=1 →
// a kanban board as Jira shows it (old done work and its backlog column
// left out), else the board; ?jql= narrows. The response is {cards, total}.
func boardCards(ctx context.Context, s *Server, r *http.Request) (any, error) {
	id, err := boardID(r)
	if err != nil {
		return nil, err
	}
	c, jql, pf := s.Client(), Q(r, "jql"), Q(r, "points")
	var cfg *jira.BoardConfig
	if pf == "" || Q(r, "kanban") != "" {
		if bc, e := c.BoardConfiguration(ctx, id); e == nil {
			cfg = bc
			pf = cmp.Or(pf, bc.PointsField)
		}
	}
	var cards []jira.Card
	var total int
	switch {
	case Q(r, "sprint") != "":
		sp, e := strconv.Atoi(Q(r, "sprint"))
		if e != nil {
			return nil, badRequest("bad sprint id")
		}
		cards, total, err = c.SprintIssues(ctx, id, sp, jql, pf)
	case Q(r, "backlog") != "":
		cards, total, err = c.BacklogIssues(ctx, id, jql, pf)
	case Q(r, "kanban") != "":
		cards, total, err = c.BoardIssues(ctx, id, andOrderedJQL(kanbanJQL(s.UIConfig().KanbanDoneDays), jql), pf)
		if i := kanbanBacklog(cfg); i >= 0 && err == nil {
			kept := cards[:0]
			for _, cd := range cards {
				if !slices.Contains(cfg.Columns[i].StatusIDs, cd.StatusID) {
					kept = append(kept, cd)
				}
			}
			total -= len(cards) - len(kept)
			cards = kept
		}
	default:
		cards, total, err = c.BoardIssues(ctx, id, jql, pf)
	}
	if err != nil {
		return nil, err
	}
	observeRules(s, r, cards)
	return map[string]any{"cards": cards, "total": total}, nil
}

// kanbanJQL hides what Jira's own kanban board hides: work done more than
// ui.kanban_done_days (two weeks by default) ago.
func kanbanJQL(days int) string {
	if days < 1 || days > 365 {
		days = 14
	}
	return fmt.Sprintf("statusCategory != Done OR updated >= -%dd", days)
}

// kanbanBacklog is the index of a kanban board's backlog column (Jira names
// it "Backlog" when the board has one), or -1.
func kanbanBacklog(cfg *jira.BoardConfig) int {
	if cfg != nil && len(cfg.Columns) > 0 && strings.EqualFold(cfg.Columns[0].Name, "backlog") {
		return 0
	}
	return -1
}
