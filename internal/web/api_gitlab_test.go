package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestGitLabMR: /api/gitlab/mr reads a merge request from its link; a link
// no instance has a token for is a 404.
func TestGitLabMR(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/projects/g/p/merge_requests/7" {
			w.Write([]byte(`{"iid": 7, "title": "Fix login", "state": "opened"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer gl.Close()
	s := New(context.Background(), Options{GitLab: gitlab.NewSites([]gitlab.Config{{BaseURL: gl.URL, Token: "tok"}})})
	rec := call(s, "GET", "/api/gitlab/mr?url="+url.QueryEscape(gl.URL+"/g/p/-/merge_requests/7"), "", "")
	var mr struct{ Title, State string }
	if err := json.Unmarshal(rec.Body.Bytes(), &mr); err != nil || rec.Code != 200 || mr.Title != "Fix login" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := call(s, "GET", "/api/gitlab/mr?url="+url.QueryEscape("https://elsewhere.test/g/p/-/merge_requests/7"), "", ""); rec.Code != 404 {
		t.Errorf("another host: %d %s", rec.Code, rec.Body)
	}
}
