package web

import (
	"cmp"
	"context"
	"net/http"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/ui"
)

// The GitLab instances as settings shows them, each signed in to, and a
// merge request read from its link (the development section's).

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
		link := r.URL.Query().Get("url")
		var c *gitlab.Client
		if s.opt.GitLab != nil {
			c = s.opt.GitLab.For(link)
		}
		ref, ok := forge.Ref{}, false
		if c != nil {
			ref, ok = c.Parse(link)
		}
		if !ok {
			return nil, httpError{http.StatusNotFound, "no GitLab token for " + cmp.Or(forge.HostOf(link), "this link")}
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
