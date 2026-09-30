package web

import (
	"context"
	"net/http"
	"strconv"

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
	o := s.opt
	me, _ := s.Client().Myself(ctx)
	return map[string]any{
		"site": o.Site, "sites": o.Sites, "demo": o.Demo, "version": o.Version,
		"baseURL": s.Client().BaseURL(), "me": me,
		"projects": o.Jira.Projects, "ui": o.UI,
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
	if qf, err := c.QuickFilters(ctx, id); err == nil {
		out["quickFilters"] = qf
	}
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

// boardCards: ?sprint=ID → that sprint, ?backlog=1 → backlog, else the
// board; ?jql= narrows. The response is {cards, total}.
func boardCards(ctx context.Context, s *Server, r *http.Request) (any, error) {
	id, err := boardID(r)
	if err != nil {
		return nil, err
	}
	c, jql, pf := s.Client(), Q(r, "jql"), Q(r, "points")
	if pf == "" {
		if cfg, e := c.BoardConfiguration(ctx, id); e == nil {
			pf = cfg.PointsField
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
	default:
		cards, total, err = c.BoardIssues(ctx, id, jql, pf)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"cards": cards, "total": total}, nil
}
