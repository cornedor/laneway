package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// fakeWaiting is a GitLab whose user ada has one review asked, and one own
// merge request with notes comments.
func fakeWaiting(t *testing.T, notes int) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mr := func(n int, title string, c int) string {
			return fmt.Sprintf(`[{"iid": %d, "title": %q, "state": "opened", "user_notes_count": %d, "author": {"name": "Ada"}, "web_url": "%s/g/p/-/merge_requests/%d"}]`, n, title, c, srv.URL, n)
		}
		switch {
		case r.URL.Path == "/api/v4/user":
			w.Write([]byte(`{"username": "ada"}`))
		case q.Get("reviewer_username") != "":
			w.Write([]byte(mr(1, "Review me", 0)))
		case q.Get("scope") == "created_by_me":
			w.Write([]byte(mr(2, "Mine", notes)))
		case strings.HasSuffix(r.URL.Path, "/merge_requests/2"):
			w.Write([]byte(fmt.Sprintf(`{"iid": 2, "title": "Mine", "state": "opened", "user_notes_count": %d, "web_url": "%s/g/p/-/merge_requests/2"}`, notes, srv.URL)))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMRScreen: alt+m lists what waits on you by why, yours only with
// comments unseen; enter reads one in the panel alone, which marks its
// comments seen, and esc goes back to the list.
func TestMRScreen(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	srv := fakeWaiting(t, 2)
	m := jiraTabModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	out, cmd := m.handleJiraKey(keyMsg(t, "alt+m"))
	m = out.(Model)
	if m.jiraTab.mrs == nil || cmd == nil {
		t.Fatal("alt+m did not open the merge requests")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Merge requests waiting on you · 2", MRReview, "g/p!1", "Review me", MRYours, "g/p!2", "2 comments"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	m.jiraTab.mrs.row = 1 // Mine
	out, cmd = m.handleJiraKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.refOpen || m.mr == nil || cmd == nil {
		t.Fatal("enter did not read it in the panel")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	out, _ = m.handleRefKey(keyMsg(t, "esc"))
	if m = out.(Model); m.refOpen || m.jiraTab.mrs == nil {
		t.Errorf("esc: panel open %v, screen %v", m.refOpen, m.jiraTab.mrs != nil)
	}
	rows, _ := MRInbox(m.ctx, m.gitlab, m.store)
	if len(rows) != 1 || rows[0].Group != MRReview {
		t.Errorf("after reading it, yours stays listed: %+v", rows)
	}
}
