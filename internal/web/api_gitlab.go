package web

import (
	"cmp"
	"context"
	"net/http"
	"strconv"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/i18n"
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
	// ?url= a merge request's link; &fresh=1 past the cache, and asks glab
	// for a missing token again. 404 with signin: no instance with a token
	// has it.
	get("/gitlab/mr", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		fresh := r.URL.Query().Get("fresh") == "1"
		if fresh && s.opt.GitLab != nil {
			s.opt.GitLab.Forget(r.URL.Query().Get("url"))
		}
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		if fresh {
			c.Invalidate(ref.Repo, ref.Number)
		}
		mr, err := c.Get(ctx, ref.Repo, ref.Number)
		if err == nil {
			ui.MRSeen(s.opt.Store, mr)
		}
		return mr, rejected(err, r)
	})
	// ?url=: {Title, Description, TargetBranch, Draft, AssigneeIDs,
	// ReviewerIDs, Labels}, each left out to keep it (TUI: e); the merge
	// request after.
	put("/gitlab/mr", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct {
			Title, Description, TargetBranch *string
			Draft                            *bool
			AssigneeIDs, ReviewerIDs         *[]int
			Labels                           *[]string
		}](r)
		if err != nil {
			return nil, err
		}
		e := gitlab.Edit{Title: b.Title, Description: b.Description, TargetBranch: b.TargetBranch,
			AssigneeIDs: b.AssigneeIDs, ReviewerIDs: b.ReviewerIDs, Labels: b.Labels}
		if b.Draft != nil {
			mr, err := c.Get(ctx, ref.Repo, ref.Number)
			if err != nil {
				return nil, err
			}
			t := gitlab.DraftTitle(cmp.Or(ptrOr(b.Title), mr.Title), *b.Draft)
			e.Title = &t
		}
		if err := c.Update(ctx, ref.Repo, ref.Number, e); err != nil {
			return nil, err
		}
		return c.Get(ctx, ref.Repo, ref.Number)
	})
	// ?url=: the project's Members (who can review or be assigned), and
	// whether the merge request takes several assignees and reviewers
	// (several when GitLab doesn't say); and its labels.
	get("/gitlab/members", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		ms, err := c.Members(ctx, ref.Repo)
		if err != nil {
			return nil, err
		}
		a, rv, merr := c.Multiple(ctx, ref.Repo, ref.Number)
		if merr != nil {
			a, rv = true, true
		}
		if ms == nil {
			ms = []gitlab.Member{}
		}
		return map[string]any{"Members": ms, "MultipleAssignees": a, "MultipleReviewers": rv}, nil
	})
	get("/gitlab/labels", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		ls, err := c.Labels(ctx, ref.Repo)
		if ls == nil {
			ls = []string{}
		}
		return ls, err
	})
	// ?url=: the ways to merge it, GitLab's default first, under Title, and
	// Ready, why it can't be merged now ("" when it can) (TUI: M).
	get("/gitlab/merge", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		c.Invalidate(ref.Repo, ref.Number) // what GitLab says now
		mr, err := c.Get(ctx, ref.Repo, ref.Number)
		if err != nil {
			return nil, err
		}
		return map[string]any{"Title": ui.MergeTitle(mr), "Ready": ui.MergeReady(mr), "Choices": ui.MergeChoices(mr)}, nil
	})
	// ?url=: {Squash, DeleteBranch} merges it; the merge request after.
	post("/gitlab/merge", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		o, err := Body[forge.MergeOptions](r)
		if err != nil {
			return nil, err
		}
		if err := c.Merge(ctx, ref.Repo, ref.Number, o); err != nil {
			return nil, err
		}
		return c.Get(ctx, ref.Repo, ref.Number)
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
			return nil, FieldError{Field: "job", Msg: i18n.T("a job id")}
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

// signInErr is a merge request on a host without a token, or whose token
// GitLab rejected: its body carries signin, how to sign in.
type signInErr struct {
	code int
	gitlab.SignIn
}

func (e signInErr) Error() string { return e.Lines()[0] }

// rejected is err as a signInErr when GitLab refused the token.
func rejected(err error, r *http.Request) error {
	if !gitlab.Rejected(err) {
		return err
	}
	si := gitlab.SignInFor(forge.HostOf(r.URL.Query().Get("url")))
	si.Rejected = true
	return signInErr{http.StatusBadGateway, si}
}

func ptrOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
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
	if h := forge.HostOf(link); h != "" && gitlab.IsMRLink(link) {
		return nil, forge.Ref{}, signInErr{http.StatusNotFound, gitlab.SignInFor(h)}
	}
	return nil, forge.Ref{}, httpError{http.StatusNotFound, i18n.Tf("no GitLab token for %s", cmp.Or(forge.HostOf(link), i18n.T("this link")))}
}
