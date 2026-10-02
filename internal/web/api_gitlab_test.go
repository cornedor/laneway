package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// TestGitLabInbox: /api/gitlab/inbox is ui.MRInbox's rows; none configured
// says so.
func TestGitLabInbox(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/user" {
			w.Write([]byte(`{"username": "ada"}`))
			return
		}
		if r.URL.Query().Get("scope") == "assigned_to_me" {
			w.Write([]byte(`[{"iid": 5, "title": "Do it", "web_url": "http://` + r.Host + `/g/p/-/merge_requests/5"}]`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer gl.Close()
	var got struct {
		Rows       []struct{ Group string }
		Configured bool
	}
	rec := call(New(context.Background(), Options{GitLab: gitlab.NewSites([]gitlab.Config{{BaseURL: gl.URL, Token: "tok"}})}), "GET", "/api/gitlab/inbox", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Rows) != 1 || got.Rows[0].Group != "Assigned to you" || !got.Configured {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = call(New(context.Background(), Options{}), "GET", "/api/gitlab/inbox", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Configured || len(got.Rows) != 0 {
		t.Errorf("none: %s", rec.Body)
	}
}

// TestGitLabDiff: /api/gitlab/diff is every file's lines, numbered and
// highlighted, a too-large one flagged, and the inline threads.
func TestGitLabDiff(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1 +1 @@\n-var x = 1\n+var x = 2\n"}, {"new_path": "big.lock", "too_large": true}]`))
		case strings.HasSuffix(r.URL.Path, "/discussions"):
			w.Write([]byte(`[{"id": "d1", "notes": [{"id": 1, "body": "ok?", "author": {"name": "Grace"}, "position": {"position_type": "text", "new_path": "a.go", "new_line": 1}}]}]`))
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Write([]byte(`[{"id": 2, "head_commit_sha": "h2"}, {"id": 1, "head_commit_sha": "h1"}]`))
		case strings.HasSuffix(r.URL.Path, "/versions/1"):
			w.Write([]byte(`{"id": 1, "head_commit_sha": "h1", "diffs": [{"new_path": "old.go", "diff": "@@ -1 +1 @@\n-a\n+b\n"}]}`))
		default:
			w.Write([]byte(`{"iid": 7, "title": "X", "diff_refs": {"head_sha": "h"}}`))
		}
	}))
	defer gl.Close()
	s := New(context.Background(), Options{GitLab: gitlab.NewSites([]gitlab.Config{{BaseURL: gl.URL, Token: "tok"}})})
	rec := call(s, "GET", "/api/gitlab/diff?url="+url.QueryEscape(gl.URL+"/g/p/-/merge_requests/7"), "", "")
	var got struct {
		Label, Title string
		Files        []DiffFile
		Threads      []DiffThread
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got.Label != "g/p!7" || got.Title != "X" || len(got.Files) != 2 || got.Files[0].Add != 1 || got.Files[0].Del != 1 || !got.Files[1].TooLarge {
		t.Errorf("diff: %+v", got)
	}
	if l := got.Files[0].Lines; len(l) != 3 || l[1].K != "-" || l[1].O != 1 || l[2].K != "+" || l[2].N != 1 || !strings.Contains(l[2].H, `<span class="hl-k">var</span>`) {
		t.Errorf("lines: %+v", got.Files[0].Lines)
	}
	if len(got.Threads) != 1 || got.Threads[0].NewLine != 1 || got.Threads[0].Notes[0].Author != "Grace" {
		t.Errorf("threads: %+v", got.Threads)
	}
	rec = call(s, "GET", "/api/gitlab/diff?version=1&url="+url.QueryEscape(gl.URL+"/g/p/-/merge_requests/7"), "", "")
	var v struct {
		Version  int
		Versions []DiffVersion
		Files    []DiffFile
		Threads  []DiffThread
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Version != 1 || len(v.Versions) != 2 || len(v.Files) != 1 || v.Files[0].Path != "old.go" || len(v.Threads) != 0 {
		t.Errorf("version 1: %s", rec.Body)
	}
}
