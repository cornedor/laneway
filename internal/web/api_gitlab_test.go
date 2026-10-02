package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestGitLabSites: /api/gitlab is each instance signed in to; none (the
// demo) is an empty list.
func TestGitLabSites(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"username": "ada"}`))
	}))
	defer gl.Close()
	var got []GitLabSite
	rec := call(New(context.Background(), Options{GitLab: gitlab.NewSites([]gitlab.Config{{BaseURL: gl.URL, Token: "tok"}})}), "GET", "/api/gitlab", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 1 || !got[0].OK || got[0].User != "ada" || got[0].From != "config" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := call(New(context.Background(), Options{}), "GET", "/api/gitlab", "", ""); rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Errorf("no instances: %q", rec.Body)
	}
}
