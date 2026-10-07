package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("no token on %s", r.URL.Path)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests/42/approvals"):
			w.Write([]byte(`{"approved": false, "approvals_required": 2, "approvals_left": 1, "approved_by": [{"user": {"name": "Grace Hopper"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/projects/220/pipelines/9/jobs"):
			// newest first, as GitLab lists them
			w.Write([]byte(`[{"name": "lint", "stage": "test", "status": "created"}, {"name": "dockerlint", "stage": "test", "status": "failed", "allow_failure": true},
				{"name": "unit", "stage": "test", "status": "failed"}, {"name": "build", "stage": "build", "status": "success"}]`))
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
	if mr.Title != "Fix the widget" || mr.Author != "Ada Lovelace" || mr.Mergeable || mr.MergeStatus != "pipeline still running · conflicts" {
		t.Errorf("change = %+v", mr)
	}
	if mr.WebURL != srv.URL+"/g/sub/p/-/merge_requests/42" {
		t.Errorf("web url %q", mr.WebURL)
	}
	if c := mr.Checks; c == nil || c.Status != forge.StatusRunning || len(c.Groups) != 2 || c.Groups[0].Name != "build" ||
		c.Groups[1].Jobs[0].Status != forge.StatusFailed || c.Groups[1].Jobs[1].Status != forge.StatusWarning || c.Groups[1].Jobs[2].Status != forge.StatusPending {
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

// TestEscapesStripped: no terminal escape in a merge request, its jobs or
// an error message survives to a render path.
func TestEscapesStripped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests/1"):
			w.Write([]byte(`{"iid": 1, "title": "\u001b]52;c;eA==\u0007hi\u009b2J"}`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests/1/approvals"):
			w.Write([]byte(`{}`))
		default:
			http.Error(w, `{"message": "no\u001b[2J"}`, http.StatusConflict)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)
	mr, err := c.Get(context.Background(), "g/p", 1)
	if err != nil || mr.Title != "]52;c;eA==hi2J" {
		t.Errorf("title %q, err %v", mr.Title, err)
	}
	if err := c.Approve(context.Background(), "g/p", 1); err == nil || strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("error %q", err)
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
	var merge map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/merge") {
			json.NewDecoder(r.Body).Decode(&merge)
		}
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
	if err := c.Merge(ctx, "g/p", 5, forge.MergeOptions{Squash: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/v4/user", "POST /api/v4/projects/g/p/merge_requests/5/approve", "PUT /api/v4/projects/g/p/merge_requests/5/merge"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if merge["squash"] != true || merge["should_remove_source_branch"] != false {
		t.Errorf("merge body = %v", merge)
	}
}

// TestUpdate: only the fields set are sent; no one is GitLab's [0], labels
// a comma list; the cached copy goes.
func TestUpdate(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v4/projects/g/p/merge_requests/5" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	c := newTestClient(srv)
	c.cache.Put("g/p", 5, &forge.Change{})
	title, none, labels := "Draft: Fix", []int{}, []string{"bug", "ui"}
	if err := c.Update(context.Background(), "g/p", 5, Edit{Title: &title, ReviewerIDs: &none, Labels: &labels}); err != nil {
		t.Fatal(err)
	}
	want := `{"labels":"bug,ui","reviewer_ids":[0],"title":"Draft: Fix"}`
	if b, _ := json.Marshal(body); string(b) != want {
		t.Errorf("body = %s, want %s", b, want)
	}
	if _, ok := c.cache.Get("g/p", 5); ok {
		t.Error("update kept the cached copy")
	}
}

func TestDraftTitle(t *testing.T) {
	for _, c := range []struct {
		in    string
		draft bool
		want  string
	}{
		{"Fix", true, "Draft: Fix"},
		{"Draft: Fix", true, "Draft: Fix"},
		{"[Draft] WIP: Fix", false, "Fix"},
		{"draft - Fix", false, "Fix"},
		{"Drafting rules", false, "Drafting rules"},
	} {
		if got := DraftTitle(c.in, c.draft); got != c.want {
			t.Errorf("DraftTitle(%q, %v) = %q, want %q", c.in, c.draft, got, c.want)
		}
	}
}

// TestMultiple: GraphQL's allowsMultiple fields, the token as a bearer; a
// merge request GraphQL doesn't find is an error.
func TestMultiple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Variables map[string]string }
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/api/graphql" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("got %s, auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if b.Variables["p"] != "g/p" || b.Variables["iid"] != "5" {
			w.Write([]byte(`{"data": {"project": null}}`))
			return
		}
		w.Write([]byte(`{"data": {"project": {"mergeRequest": {"allowsMultipleAssignees": false, "allowsMultipleReviewers": true}}}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	a, r, err := c.Multiple(context.Background(), "g/p", 5)
	if err != nil || a || !r {
		t.Errorf("Multiple = %v %v %v", a, r, err)
	}
	if _, _, err := c.Multiple(context.Background(), "g/q", 5); err == nil {
		t.Error("no merge request: no error")
	}
}

// TestMembersLabels: every page read, members once each and by name.
func TestMembersLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch {
		case strings.HasSuffix(r.URL.Path, "/members/all") && page == "1":
			var us []string
			for i := range 100 {
				us = append(us, fmt.Sprintf(`{"id": %d, "username": "u%d", "name": "User %03d"}`, i+10, i, i))
			}
			w.Write([]byte("[" + strings.Join(us, ",") + "]"))
		case strings.HasSuffix(r.URL.Path, "/members/all"):
			w.Write([]byte(`[{"id": 1, "username": "ada", "name": "ada Lovelace"}, {"id": 10, "username": "u0", "name": "User 000"}]`))
		case strings.HasSuffix(r.URL.Path, "/labels"):
			w.Write([]byte(`[{"name": "bug"}, {"name": "ui"}]`))
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)
	ms, err := c.Members(context.Background(), "g/p")
	if err != nil || len(ms) != 101 || ms[0].Username != "ada" {
		t.Fatalf("Members = %d, first %+v, %v", len(ms), ms[0], err)
	}
	ls, err := c.Labels(context.Background(), "g/p")
	if err != nil || strings.Join(ls, ",") != "bug,ui" {
		t.Errorf("Labels = %v, %v", ls, err)
	}
}

// TestSearch: merge requests whose title names the key; ABC-12 is not
// ABC-1, and the project comes from the link.
func TestSearch(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v4/merge_requests" || q.Get("search") != "ABC-1" || q.Get("scope") != "all" || q.Get("in") != "title" {
			t.Errorf("search %s", r.URL)
		}
		w.Write([]byte(`[
			{"iid": 3, "title": "ABC-1: fix login", "state": "merged", "source_branch": "issue/ABC-1-login", "target_branch": "main", "web_url": "` + srv.URL + `/g/sub/p/-/merge_requests/3"},
			{"iid": 4, "title": "ABC-12 other", "state": "opened", "web_url": "` + srv.URL + `/g/p/-/merge_requests/4"}]`))
	}))
	defer srv.Close()
	got, err := newTestClient(srv).Search(context.Background(), "ABC-1")
	if err != nil || len(got) != 1 || got[0].Repo != "g/sub/p" || got[0].Number != 3 || got[0].State != forge.StateMerged || got[0].WebURL == "" {
		t.Fatalf("Search = %+v, %v", got, err)
	}
}

// TestWaiting: review asked of the token's user, assigned, and its own,
// each merge request once, in the first group it is in.
func TestWaiting(t *testing.T) {
	var srv *httptest.Server
	mr := func(n int) string {
		return fmt.Sprintf(`{"iid": %d, "title": "MR %d", "state": "opened", "web_url": "%s/g/p/-/merge_requests/%d"}`, n, n, srv.URL, n)
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/v4/user":
			w.Write([]byte(`{"username": "ada"}`))
		case q.Get("reviewer_username") == "ada":
			w.Write([]byte("[" + mr(1) + "," + mr(2) + "]"))
		case q.Get("scope") == "assigned_to_me":
			w.Write([]byte("[" + mr(2) + "," + mr(3) + "]"))
		case q.Get("scope") == "created_by_me":
			w.Write([]byte("[" + mr(4) + "]"))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	w, err := newTestClient(srv).Waiting(context.Background())
	if err != nil || len(w.Review) != 2 || len(w.Assigned) != 1 || w.Assigned[0].Number != 3 || len(w.Mine) != 1 || w.Mine[0].Repo != "g/p" {
		t.Fatalf("Waiting = %+v, %v", w, err)
	}
}

// TestJobLog: a job's state and its log, read uncached; a log past the cap
// keeps its end, from a line's start.
func TestJobLog(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/api/v4/projects/g/p/jobs/7":
			w.Write([]byte(`{"id": 7, "name": "unit", "stage": "test", "status": "running", "duration": 12.5, "web_url": "https://x/-/jobs/7"}`))
		case "/api/v4/projects/g/p/jobs/7/trace":
			w.Write([]byte(strings.Repeat("old line\n", jobLogMax/9+10) + "the end\n"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)
	j, err := c.JobLog(context.Background(), "g/p", 7)
	if err != nil {
		t.Fatal(err)
	}
	if j.Name != "unit" || j.Status != forge.StatusRunning || j.Done() || j.Duration != 12 || !j.Truncated ||
		!strings.HasPrefix(j.Log, "old line\n") || !strings.HasSuffix(j.Log, "the end\n") || len(j.Log) > jobLogMax {
		t.Errorf("job = %+v (log %d bytes)", j.Job, len(j.Log))
	}
	if _, err := c.JobLog(context.Background(), "g/p", 7); err != nil || calls != 4 {
		t.Errorf("a second read: %v, %d calls (want 4: uncached)", err, calls)
	}
}

// TestTokenStaysOnRedirect: a redirect to another host gets no token.
func TestTokenStaysOnRedirect(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization") + r.Header.Get("PRIVATE-TOKEN"); h != "" {
			t.Errorf("token sent to the redirect host: %q", h)
		}
		w.Write([]byte(mrJSON))
	}))
	defer other.Close()
	away := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, away+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	newTestClient(srv).Get(context.Background(), "g/p", 42)
}
