package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/jira"
)

// TestDevWithGitLab: without a pull request from Jira, the GitLab merge
// requests naming the key are added as ones; with one, nothing is searched.
func TestDevWithGitLab(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	searched := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searched++
		w.Write([]byte(`[{"iid": 9, "title": "ABC-1 fix", "state": "opened", "draft": true, "source_branch": "issue/ABC-1", "target_branch": "main",
			"web_url": "http://` + r.Host + `/g/p/-/merge_requests/9", "author": {"name": "Ada"}}]`))
	}))
	defer srv.Close()
	s := gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}})
	got := DevWithGitLab(context.Background(), []jira.DevItem{{Kind: "branch", Name: "issue/ABC-1"}}, s, "ABC-1")
	if len(got) != 2 || got[1].Kind != "pr" || got[1].Status != "DRAFT" || got[1].Repo != "g/p" || got[1].Branch != "issue/ABC-1 → main" || got[1].Author != "Ada" {
		t.Fatalf("got %+v", got)
	}
	if got := DevWithGitLab(context.Background(), []jira.DevItem{{Kind: "pr"}}, s, "ABC-1"); len(got) != 1 || searched != 1 {
		t.Errorf("with a PR from Jira: %+v, searched %d times", got, searched)
	}
	if got := DevWithGitLab(context.Background(), nil, nil, "ABC-1"); got != nil {
		t.Errorf("no GitLab: %+v", got)
	}
}
