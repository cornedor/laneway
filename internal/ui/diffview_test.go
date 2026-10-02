package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestDiffView: d on a merge request in the panel shows its diff over the
// body: the files, the changed lines with their numbers, an inline thread
// under its line; z folds a file, esc goes back to the merge request.
func TestDiffView(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path": "main.go", "new_path": "main.go", "diff": "@@ -1,3 +1,3 @@\n package main\n-var a = 1\n+var a = 2\n"},
				{"old_path": "README.md", "new_path": "README.md", "new_file": true, "diff": "@@ -0,0 +1 @@\n+# hi\n"}]`))
		case strings.HasSuffix(r.URL.Path, "/discussions"):
			w.Write([]byte(`[{"id": "d1", "notes": [{"id": 1, "body": "Why 2?", "author": {"name": "Grace"},
				"position": {"position_type": "text", "new_path": "main.go", "old_path": "main.go", "new_line": 2}}]}]`))
		case strings.HasSuffix(r.URL.Path, "/approvals"), strings.HasSuffix(r.URL.Path, "/jobs"):
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{"iid": 7, "project_id": 3, "title": "Bump a", "state": "opened", "diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}}`))
		}
	}))
	defer srv.Close()
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	c, r, ok := m.gitlabMR(srv.URL + "/g/p/-/merge_requests/7")
	if !ok {
		t.Fatal("no client for the fake GitLab")
	}
	out, _ := m.Update(m.showMR(c, r, srv.URL+"/g/p/-/merge_requests/7", "Bump a")())
	m = out.(Model)
	out, cmd := m.handleKey(keyMsg(t, "d"))
	m = out.(Model)
	if m.diff == nil || cmd == nil {
		t.Fatal("d did not open the diff")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"g/p!7 · Bump a", "main.go", "README.md", "var a = 1", "var a = 2", "Grace", "Why 2?", "# hi"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	out, _ = m.handleKey(keyMsg(t, "z"))
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "lines folded") {
		t.Error("z did not fold the file")
	}
	out, _ = m.handleKey(keyMsg(t, "esc"))
	if m = out.(Model); m.diff != nil || m.mr == nil {
		t.Errorf("esc: diff %v, merge request %v", m.diff != nil, m.mr != nil)
	}
}
