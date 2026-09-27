package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// ctrl+r shows the issues of the pull and merge requests waiting on your
// review as a view: gh's and glab's, the keys found in their titles and
// branches. Their cards get a ⌥ for the rest of the session.

// reviewRequest is a pull or merge request asking for your review.
type reviewRequest struct{ title, branch string }

// reviewRequests asks gh and glab for what waits on your review; a tool
// that's missing or fails is left out, an error only when both do.
var reviewRequests = func(ctx context.Context) ([]reviewRequest, error) {
	var out []reviewRequest
	var errs []error
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
	if me, err := exec.CommandContext(ctx, "glab", "api", "user").Output(); err == nil {
		var u struct{ Username string }
		_ = json.Unmarshal(me, &u)
		b, err := exec.CommandContext(ctx, "glab", "api", "merge_requests?scope=all&state=opened&per_page=100&reviewer_username="+u.Username).Output()
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
	if len(errs) == 2 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// cliError is a command's stderr's first line, else its error.
func cliError(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if line, _, _ := strings.Cut(strings.TrimSpace(string(ee.Stderr)), "\n"); line != "" {
			return line
		}
	}
	return err.Error()
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
		for _, k := range commitIssueRe.FindAllString(r.title, -1) {
			add(k)
		}
		add(branchKey(r.branch))
	}
	return keys
}

// reviewMsg is the keys waiting on your review.
type reviewMsg struct {
	keys []string
	err  error
}

// openReview asks gh and glab, then shows the keys found as a view.
func (m *Model) openReview() tea.Cmd {
	if m.jiraTab.cfg == nil {
		m.status = "open a board first"
		return nil
	}
	m.status = "asking gh and glab what waits on your review…"
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		reqs, err := reviewRequests(ctx)
		if err != nil {
			return reviewMsg{err: err}
		}
		projects, err := c.ListProjects(ctx)
		return reviewMsg{keys: reviewKeys(reqs, projects), err: err}
	}
}

func (m Model) handleReview(msg reviewMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail("review requests: " + msg.err.Error())
		return m, nil
	}
	m.reviewKeys = map[string]bool{}
	for _, k := range msg.keys {
		m.reviewKeys[k] = true
	}
	if len(msg.keys) == 0 {
		m.status = "nothing waits on your review (no issue keys in the requests' titles or branches)"
		return m, nil
	}
	m.status = ""
	return m, m.runNamedJQLView("Review: waiting on me", "key in ("+strings.Join(msg.keys, ", ")+") ORDER BY updated DESC")
}
