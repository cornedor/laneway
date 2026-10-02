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
		case strings.Contains(r.URL.Path, "/repository/files/"):
			w.Write([]byte("package main\n\nvar a = 2\nvar b = 9\n"))
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Write([]byte(`[{"id": 2, "head_commit_sha": "h2"}, {"id": 1, "head_commit_sha": "h1aaaaaaa"}]`))
		case strings.HasSuffix(r.URL.Path, "/versions/1"):
			w.Write([]byte(`{"id": 1, "head_commit_sha": "h1", "diffs": [{"old_path": "main.go", "new_path": "main.go", "diff": "@@ -1 +1 @@\n-var a = 1\n+var a = 3\n"}]}`))
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
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "loading diff") { // drawn before it lands
		t.Errorf("loading:\n%s", view)
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"g/p!7 · Bump a", "main.go", "README.md", "var a = 1", "var a = 2", "Grace", "Why 2?", "# hi"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	out, _ = m.handleKey(keyMsg(t, "v"))
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !m.diff.picking || !strings.Contains(view, "version 2 (newest)") || !strings.Contains(view, "version 1") {
		t.Fatalf("v: no versions:\n%s", view)
	}
	out, _ = m.handleKey(keyMsg(t, "down"))
	out, cmd = out.(Model).handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "version 1 of 2") || !strings.Contains(view, "var a = 3") || strings.Contains(view, "Why 2?") || strings.Contains(view, "README.md") {
		t.Fatalf("version 1: its own diff, no threads:\n%s", view)
	}
	out, _ = m.handleKey(keyMsg(t, "v"))
	out, _ = out.(Model).handleKey(keyMsg(t, "up"))
	out, cmd = out.(Model).handleKey(keyMsg(t, "enter"))
	out, _ = out.(Model).Update(cmd())
	m = out.(Model)
	m.diff.setPos(1) // in main.go
	out, cmd = m.handleKey(keyMsg(t, "e"))
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "var b = 9") || strings.Contains(view, "@@ -1,3 +1,3 @@") {
		t.Fatalf("e: not the whole file:\n%s", view)
	}
	out, cmd = m.handleKey(keyMsg(t, "e"))
	out, _ = out.(Model).Update(cmd())
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "var b = 9") {
		t.Fatalf("e again: still whole:\n%s", view)
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
