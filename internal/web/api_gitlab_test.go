package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/herdr"
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
		case strings.Contains(r.URL.Path, "/repository/files/a.go/raw"):
			w.Write([]byte("var x = 2\nvar y = 3\n"))
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
	rec = call(s, "GET", "/api/gitlab/diff/file?path=a.go&url="+url.QueryEscape(gl.URL+"/g/p/-/merge_requests/7"), "", "")
	var f DiffFile
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil || rec.Code != 200 || len(f.Lines) != 3 || f.Lines[2].N != 2 || f.Lines[2].O != 2 || f.Lines[2].K != " " {
		t.Errorf("whole file: %d %s", rec.Code, rec.Body)
	}
}

// TestGitLabNoteResolve: notes go into the pending review, a new one anchored
// to the diff's commits; resolve PUTs the thread; a pending note is dropped,
// the review submitted with its verdict, an approval given alone.
func TestGitLabNoteResolve(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	var writes []string
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			b, _ := io.ReadAll(r.Body)
			writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
			w.Write([]byte(`{}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/diffs") {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`{"iid": 7, "diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}}`))
	}))
	defer gl.Close()
	s := New(context.Background(), Options{GitLab: gitlab.NewSites([]gitlab.Config{{BaseURL: gl.URL, Token: "tok"}})})
	q := "?url=" + url.QueryEscape(gl.URL+"/g/p/-/merge_requests/7")
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/gitlab/note" + q, `{"Body": "why?", "NewPath": "a.go", "OldPath": "a.go", "NewLine": 3}`},
		{"POST", "/api/gitlab/note" + q, `{"Body": "ok", "ReplyTo": "d1"}`},
		{"POST", "/api/gitlab/note" + q, `{"Body": "both", "NewPath": "a.go", "OldPath": "a.go", "NewLine": 4,
			"Range": {"Start": {"NewLine": 3, "OldPos": 3, "NewPos": 3}, "End": {"NewLine": 4, "OldPos": 3, "NewPos": 4}}}`},
		{"PUT", "/api/gitlab/resolve" + q + "&thread=d1", `{"Resolved": true}`},
	} {
		if rec := call(s, c.method, c.path, c.body, ""); rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
	if rec := call(s, "POST", "/api/gitlab/note"+q, `{"Body": " "}`, ""); rec.Code != 400 {
		t.Errorf("empty note: %d", rec.Code)
	}
	for _, c := range []struct{ method, path, body string }{
		{"DELETE", "/api/gitlab/draft" + q + "&draft=4", ""},
		{"POST", "/api/gitlab/review" + q, `{"Verdict": "changes", "Summary": "see notes"}`},
		{"POST", "/api/gitlab/review" + q, `{"Verdict": "approve", "Only": true}`},
	} {
		if rec := call(s, c.method, c.path, c.body, ""); rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
	if rec := call(s, "POST", "/api/gitlab/review"+q, `{"Verdict": "lgtm"}`, ""); rec.Code != 400 {
		t.Errorf("an unknown verdict: %d", rec.Code)
	}
	want := []string{"POST /api/v4/projects/g/p/merge_requests/7/draft_notes", "POST /api/v4/projects/g/p/merge_requests/7/draft_notes",
		"POST /api/v4/projects/g/p/merge_requests/7/draft_notes", "PUT /api/v4/projects/g/p/merge_requests/7/discussions/d1", "DELETE /api/v4/projects/g/p/merge_requests/7/draft_notes/4",
		"POST /api/v4/projects/g/p/merge_requests/7/draft_notes/bulk_publish", "POST /api/v4/projects/g/p/merge_requests/7/approve"}
	if len(writes) != len(want) {
		t.Fatalf("writes:\n%s", strings.Join(writes, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(writes[i], w+" ") {
			t.Errorf("write %d: %s, want %s", i, writes[i], w)
		}
	}
	if !strings.Contains(writes[0], `"head_sha":"h"`) || !strings.Contains(writes[0], `"new_line":3`) || !strings.Contains(writes[1], `"in_reply_to_discussion_id":"d1"`) ||
		!strings.Contains(writes[2], `"line_range":{"end":{"line_code":"`) || !strings.Contains(writes[2], `_3_4","new_line":4,"type":"new"}`) ||
		!strings.Contains(writes[5], `"reviewer_state":"requested_changes"`) || !strings.Contains(writes[5], `"note":"see notes"`) {
		t.Errorf("bodies:\n%s", strings.Join(writes, "\n"))
	}
}

// TestGitLabDraftEditAgent: a pending note reworded; an agent review without
// herdr says so.
func TestGitLabDraftEditAgent(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	old := herdrClient
	herdrClient = func() *herdr.Client { return nil }
	t.Cleanup(func() { herdrClient = old })
	var writes []string
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		w.Write([]byte(`{}`))
	}))
	defer gl.Close()
	s := New(context.Background(), Options{GitLab: gitlab.NewSites([]gitlab.Config{{BaseURL: gl.URL, Token: "tok"}})})
	q := "?url=" + url.QueryEscape(gl.URL+"/g/p/-/merge_requests/7")
	if rec := call(s, "PUT", "/api/gitlab/draft"+q+"&draft=4", `{"Body": "reworded"}`, ""); rec.Code != 200 {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	if len(writes) != 1 || !strings.HasPrefix(writes[0], "PUT /api/v4/projects/g/p/merge_requests/7/draft_notes/4 ") || !strings.Contains(writes[0], `"note":"reworded"`) {
		t.Errorf("writes: %q", writes)
	}
	if rec := call(s, "POST", "/api/gitlab/agent-review"+q, `{}`, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no herdr: %d %s", rec.Code, rec.Body)
	}
}
