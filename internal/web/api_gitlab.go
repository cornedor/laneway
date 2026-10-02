package web

import (
	"context"
	"net/http"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// The GitLab instances as settings shows them: each signed in to.

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
}

func gitlabSite(st gitlab.Status) GitLabSite {
	return GitLabSite{Host: st.Host, BaseURL: st.BaseURL, From: st.From, User: st.User.Username, Summary: st.Summary(), OK: st.Err == nil}
}
