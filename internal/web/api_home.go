package web

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/home"
	"github.com/cornedor/laneway/internal/jira"
)

// The start screen (ui.home): its widgets ride on the session, these answer
// the two that need working out. The rest read /work, /inbox and /timer as
// their own views do.

func init() {
	// GET /home/sprint/{board}: the active sprint's health, null without one.
	get("/home/sprint/{board}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		id, err := boardID(r)
		if err != nil {
			return nil, err
		}
		c := s.Client()
		sprints, err := c.Sprints(ctx, id)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(sprints, func(x jira.Sprint) bool { return x.State == "active" })
		if i < 0 {
			return nil, nil
		}
		cfg, err := c.BoardConfiguration(ctx, id)
		if err != nil {
			return nil, err
		}
		issues, err := c.SprintBurn(ctx, sprints[i].ID, cfg.PointsField)
		if err != nil {
			return nil, err
		}
		h := home.Health(sprints[i], issues, time.Now())
		return map[string]any{"Sprint": h, "Progress": h.Progress(), "Behind": h.Behind()}, nil
	})
	// GET /home/filters: a count per starred Jira filter and starred search.
	get("/home/filters", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		fs, err := home.Filters(ctx, s.Client(), !strings.EqualFold(strings.TrimSpace(s.UIConfig().SavedFilters), "off"), starredJQL(s))
		return nonNil(fs), err
	})
}

// homeInfo is the session's home: the widgets the screen shows (every one
// when ui.home names none) and whether it is the start screen.
func homeInfo(s *Server) map[string]any {
	picked := home.Pick(s.UIConfig().Home)
	if len(picked) == 0 {
		return map[string]any{"Widgets": home.Widgets, "Start": false}
	}
	return map[string]any{"Widgets": picked, "Start": true}
}
