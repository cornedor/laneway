// Package review finds what waits on your review, for the TUI's ctrl+r and
// the web's review screen: the pull and merge requests gh and glab list for
// you, and the issue keys their titles and branches name.
package review

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"slices"
	"strings"

	"github.com/cornedor/laneway/internal/cli"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// Request is a pull or merge request asking for your review.
type Request struct{ Title, Branch string }

// Requests asks gh and glab; a tool that's missing or fails is left out,
// an error only when both do. Tests replace it.
var Requests = func(ctx context.Context) ([]Request, error) {
	var out []Request
	var errs []error
	if cli.Have("gh") {
		if b, err := exec.CommandContext(ctx, "gh", "search", "prs", "--review-requested=@me", "--state=open",
			"--json", "title", "--limit", "100").Output(); err == nil {
			var prs []struct{ Title string }
			if err := json.Unmarshal(b, &prs); err != nil {
				errs = append(errs, err)
			}
			for _, p := range prs {
				out = append(out, Request{Title: p.Title})
			}
		} else {
			errs = append(errs, errors.New("gh: "+cli.Error(err)))
		}
	} else {
		errs = append(errs, errors.New("gh: not installed"))
	}
	if cli.Have("glab") {
		if me, err := exec.CommandContext(ctx, "glab", "api", "user").Output(); err == nil {
			var u struct{ Username string }
			_ = json.Unmarshal(me, &u)
			b, err := exec.CommandContext(ctx, "glab", "api", ReviewerMRsPath(u.Username)).Output()
			var mrs []struct {
				Title        string `json:"title"`
				SourceBranch string `json:"source_branch"`
			}
			if err == nil {
				err = json.Unmarshal(b, &mrs)
			}
			if err != nil {
				errs = append(errs, errors.New("glab: "+cli.Error(err)))
			}
			for _, mr := range mrs {
				out = append(out, Request{Title: mr.Title, Branch: mr.SourceBranch})
			}
		} else {
			errs = append(errs, errors.New("glab: "+cli.Error(err)))
		}
	} else {
		errs = append(errs, errors.New("glab: not installed"))
	}
	if len(errs) == 2 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// Keys are the keys the requests name in projects, each once.
func Keys(reqs []Request, projects []jira.Project) []string {
	var keys []string
	add := func(k string) {
		p, _, _ := strings.Cut(k, "-")
		known := slices.ContainsFunc(projects, func(pr jira.Project) bool { return pr.Key == p })
		if k != "" && known && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, r := range reqs {
		for _, k := range work.KeyRe.FindAllString(r.Title, -1) {
			add(k)
		}
		add(work.BranchKey(r.Branch))
	}
	return keys
}

// ReviewerMRsPath is glab api's path to the open merge requests user is a
// reviewer on.
func ReviewerMRsPath(user string) string {
	return "merge_requests?scope=all&state=opened&per_page=100&reviewer_username=" + url.QueryEscape(user)
}
