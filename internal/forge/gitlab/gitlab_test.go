package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/forge"
)

// newTestClient points a Client at srv with a dummy token (so Enabled is true).
func newTestClient(srv *httptest.Server) *Client {
	return New(Config{BaseURL: srv.URL, Token: "tok"})
}

const mrJSON = `{
  "iid": 42, "project_id": 220, "title": "Fix the widget", "state": "opened",
  "source_branch": "fix/widget", "target_branch": "main",
  "labels": ["bug"], "changes_count": "3", "detailed_merge_status": "ci_still_running",
  "has_conflicts": true, "description": "Some **bold** detail.",
  "updated_at": "2026-06-15T06:52:20.294Z",
  "author": {"name": "Ada Lovelace", "username": "ada"},
  "reviewers": [{"name": "Grace Hopper", "username": "grace"}],
  "head_pipeline": {"id": 9, "status": "running", "duration": 251, "detailed_status": {"label": "running"}}
}`

// TestGet: the merge request, its jobs by stage and its approvals, with the
// project path encoded and the token sent; a second Get is cached.
func TestGet(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			t.Errorf("no PRIVATE-TOKEN on %s", r.URL.Path)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests/42/approvals"):
			w.Write([]byte(`{"approved": false, "approvals_required": 2, "approvals_left": 1, "approved_by": [{"user": {"name": "Grace Hopper"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/projects/220/pipelines/9/jobs"):
			w.Write([]byte(`[{"name": "build", "stage": "build", "status": "success"}, {"name": "unit", "stage": "test", "status": "failed"}, {"name": "lint", "stage": "test", "status": "created"}]`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests/42"):
			if !strings.Contains(r.RequestURI, "/projects/g%2Fsub%2Fp/") {
				t.Errorf("project path not encoded: %s", r.RequestURI)
			}
			w.Write([]byte(mrJSON))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)
	mr, err := c.Get(context.Background(), "g/sub/p", 42)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Title != "Fix the widget" || mr.Author != "Ada Lovelace" || mr.Mergeable || mr.MergeStatus != "ci still running · conflicts" {
		t.Errorf("change = %+v", mr)
	}
	if mr.WebURL != srv.URL+"/g/sub/p/-/merge_requests/42" {
		t.Errorf("web url %q", mr.WebURL)
	}
	if mr.Checks == nil || mr.Checks.Status != forge.StatusRunning || len(mr.Checks.Groups) != 2 || mr.Checks.Groups[1].Jobs[1].Status != forge.StatusPending {
		t.Errorf("checks = %+v", mr.Checks)
	}
	if mr.Approvals == nil || mr.Approvals.Left != 1 || len(mr.Approvals.By) != 1 {
		t.Errorf("approvals = %+v", mr.Approvals)
	}
	n := calls
	if _, err := c.Get(context.Background(), "g/sub/p", 42); err != nil || calls != n {
		t.Errorf("second Get made %d calls, err %v", calls-n, err)
	}
}

// TestGetErrors: no token is ErrNotConfigured; a 404 says not found.
func TestGetErrors(t *testing.T) {
	if _, err := New(Config{BaseURL: "git.example.com"}).Get(context.Background(), "g/p", 1); !errors.Is(err, forge.ErrNotConfigured) {
		t.Errorf("no token: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "404 Not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Get(context.Background(), "g/p", 7)
	var se *forge.StatusErr
	if !errors.As(err, &se) || se.Code != http.StatusNotFound || !strings.Contains(err.Error(), "g/p!7 not found") {
		t.Errorf("404: %v", err)
	}
}

// TestParse: a merge request link on this instance, nested groups whole;
// another host or another page is none.
func TestParse(t *testing.T) {
	c := New(Config{BaseURL: "https://Git.Example.com/", Token: "tok"})
	for link, want := range map[string]forge.Ref{
		"https://git.example.com/g/sub/p/-/merge_requests/12":         {Repo: "g/sub/p", Number: 12},
		"https://git.example.com/g/p/-/merge_requests/3/diffs?view=x": {Repo: "g/p", Number: 3},
		"https://other.example.com/g/p/-/merge_requests/3":            {},
		"https://git.example.com/g/p/-/issues/3":                      {},
	} {
		got, ok := c.Parse(link)
		if got != want || ok != (want.Number > 0) {
			t.Errorf("Parse(%q) = %+v %v, want %+v", link, got, ok, want)
		}
	}
}

// TestMeApproveMerge: Me reads /user; approve and merge post where GitLab
// wants them and drop the cached copy.
func TestMeApproveMerge(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/v4/user" {
			w.Write([]byte(`{"username": "ada", "name": "Ada Lovelace"}`))
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()
	if u, err := c.Me(ctx); err != nil || u.Username != "ada" {
		t.Fatalf("Me = %+v, %v", u, err)
	}
	c.cache.Put("g/p", 5, &forge.Change{})
	if err := c.Approve(ctx, "g/p", 5); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.cache.Get("g/p", 5); ok {
		t.Error("approve kept the cached copy")
	}
	if err := c.Merge(ctx, "g/p", 5); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/v4/user", "POST /api/v4/projects/g/p/merge_requests/5/approve", "PUT /api/v4/projects/g/p/merge_requests/5/merge"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
