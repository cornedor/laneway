package web

import (
	"cmp"
	"context"
	"net/http"
	"strconv"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/ui"
)

// The GitLab instances as settings shows them, each signed in to, and a
// merge request read from its link (the development section's).

// JobView is a CI job's log view: Done once nothing more will come.
type JobView struct {
	ID                          int
	Name, Stage, Status, WebURL string
	Duration                    int
	HTML                        string
	Truncated, Done             bool
}

// GitLabSite is one instance's check.
type GitLabSite struct {
	Host, BaseURL, From, User, Summary string
	OK                                 bool
}

func init() {
	get("/gitlab", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		out := []GitLabSite{}
		if s.opt.GitLab == nil {
			return out, nil
		}
		for _, st := range s.opt.GitLab.Check(ctx) {
			out = append(out, gitlabSite(st))
		}
		return out, nil
	})
	// ?url= a merge request's link; &fresh=1 past the cache. 404: no
	// instance with a token has it.
	get("/gitlab/mr", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		if r.URL.Query().Get("fresh") == "1" {
			c.Invalidate(ref.Repo, ref.Number)
		}
		mr, err := c.Get(ctx, ref.Repo, ref.Number)
		if err == nil {
			ui.MRSeen(s.opt.Store, mr)
		}
		return mr, err
	})
	// ?url= a merge request's link, &job= one of its pipeline's jobs: its
	// state and its log as HTML (ansiHTML), read fresh, so a page polling a
	// running job sees its log grow.
	get("/gitlab/job", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		id, _ := strconv.Atoi(r.URL.Query().Get("job"))
		if id <= 0 {
			return nil, FieldError{Field: "job", Msg: "a job id"}
		}
		j, err := c.JobLog(ctx, ref.Repo, id)
		if err != nil {
			return nil, err
		}
		return JobView{ID: j.ID, Name: j.Name, Stage: j.Stage, Status: j.Status, Duration: j.Duration, WebURL: j.WebURL,
			HTML: ansiHTML(j.Log), Truncated: j.Truncated, Done: j.Done()}, nil
	})
	// The merge requests waiting on you, as the TUI's alt+m.
	get("/gitlab/inbox", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		rows, errs := ui.MRInbox(ctx, s.opt.GitLab, s.opt.Store)
		if rows == nil {
			rows = []ui.MRRow{}
		}
		return map[string]any{"Rows": rows, "Errs": errs, "Configured": s.opt.GitLab != nil}, nil
	})
}

func gitlabSite(st gitlab.Status) GitLabSite {
	return GitLabSite{Host: st.Host, BaseURL: st.BaseURL, From: st.From, User: st.User.Username, Summary: st.Summary(), OK: st.Err == nil}
}

// gitlabLink is the client and merge request for ?url=, a 404 when no
// instance with a token has it.
func gitlabLink(s *Server, r *http.Request) (*gitlab.Client, forge.Ref, error) {
	link := r.URL.Query().Get("url")
	var c *gitlab.Client
	if s.opt.GitLab != nil {
		c = s.opt.GitLab.For(link)
	}
	if c != nil {
		if ref, ok := c.Parse(link); ok {
			return c, ref, nil
		}
	}
	return nil, forge.Ref{}, httpError{http.StatusNotFound, "no GitLab token for " + cmp.Or(forge.HostOf(link), "this link")}
}
