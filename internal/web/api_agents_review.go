package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/cornedor/laneway/internal/cli"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/review"
)

// Waiting on my review: the issues named by the pull and merge requests gh
// and glab say await you (the TUI's ctrl+r).

func init() { get("/review", reviewQueue) }

func reviewQueue(ctx context.Context, s *Server, r *http.Request) (any, error) {
	if !cli.Have("gh") && !cli.Have("glab") {
		return nil, httpError{http.StatusNotImplemented, "neither gh nor glab is installed"}
	}
	reqs, err := review.Requests(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := s.Client().ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	keys := review.Keys(reqs, projects)
	cards := []jira.Card{}
	if len(keys) > 0 {
		if cards, err = s.Client().SearchCards(ctx, "key in ("+strings.Join(keys, ", ")+") ORDER BY updated DESC"); err != nil {
			return nil, err
		}
	}
	return map[string]any{"Keys": keys, "Cards": cards, "Requests": len(reqs)}, nil
}
