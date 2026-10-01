package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"slices"
	"strings"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// Waiting on my review: the issues named by the pull and merge requests gh
// and glab say await you (the TUI's ctrl+r).

type reviewRequest struct{ title, branch string }

// reviewRequests asks gh and glab; a tool that's missing or fails is left
// out, an error only when both do.
var reviewRequests = func(ctx context.Context) ([]reviewRequest, error) {
	var out []reviewRequest
	var errs []error
	if have("gh") {
		if b, err := exec.CommandContext(ctx, "gh", "search", "prs", "--review-requested=@me", "--state=open",
			"--json", "title", "--limit", "100").Output(); err == nil {
			var prs []struct{ Title string }
			if err := json.Unmarshal(b, &prs); err != nil {
				errs = append(errs, err)
			}
			for _, p := range prs {
				out = append(out, reviewRequest{title: p.Title})
			}
		} else {
			errs = append(errs, errors.New("gh: "+cliError(err)))
		}
	} else {
		errs = append(errs, errors.New("gh: not installed"))
	}
	if have("glab") {
		if me, err := exec.CommandContext(ctx, "glab", "api", "user").Output(); err == nil {
			var u struct{ Username string }
			_ = json.Unmarshal(me, &u)
			path := "merge_requests?scope=all&state=opened&per_page=100&reviewer_username=" + url.QueryEscape(u.Username)
			b, err := exec.CommandContext(ctx, "glab", "api", path).Output()
			var mrs []struct {
				Title        string `json:"title"`
				SourceBranch string `json:"source_branch"`
			}
			if err == nil {
				err = json.Unmarshal(b, &mrs)
			}
			if err != nil {
				errs = append(errs, errors.New("glab: "+cliError(err)))
			}
			for _, mr := range mrs {
				out = append(out, reviewRequest{title: mr.Title, branch: mr.SourceBranch})
			}
		} else {
			errs = append(errs, errors.New("glab: "+cliError(err)))
		}
	} else {
		errs = append(errs, errors.New("glab: not installed"))
	}
	if len(errs) == 2 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// reviewKeys are the keys the requests name in projects, each once.
func reviewKeys(reqs []reviewRequest, projects []jira.Project) []string {
	var keys []string
	add := func(k string) {
		p, _, _ := strings.Cut(k, "-")
		known := slices.ContainsFunc(projects, func(pr jira.Project) bool { return pr.Key == p })
		if k != "" && known && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, r := range reqs {
		for _, k := range work.KeyRe.FindAllString(r.title, -1) {
			add(k)
		}
		add(branchKey(r.branch))
	}
	return keys
}

func init() { get("/review", review) }

func review(ctx context.Context, s *Server, r *http.Request) (any, error) {
	if !have("gh") && !have("glab") {
		return nil, httpError{http.StatusNotImplemented, "neither gh nor glab is installed"}
	}
	reqs, err := reviewRequests(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := s.Client().ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	keys := reviewKeys(reqs, projects)
	cards := []jira.Card{}
	if len(keys) > 0 {
		if cards, err = s.Client().SearchCards(ctx, "key in ("+strings.Join(keys, ", ")+") ORDER BY updated DESC"); err != nil {
			return nil, err
		}
	}
	return map[string]any{"Keys": keys, "Cards": cards, "Requests": len(reqs)}, nil
}
