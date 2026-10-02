package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestDiffDiscussions: a general thread and an outdated one show under
// Discussions, the outdated one marked; c on one replies, R resolves it.
func TestDiffDiscussions(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
			mu.Unlock()
			w.Write([]byte(`{}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1 +1 @@\n-x\n+y\n"}]`))
		case strings.HasSuffix(r.URL.Path, "/discussions"):
			w.Write([]byte(`[{"id": "g1", "notes": [{"id": 1, "body": "Looks good overall", "author": {"name": "Grace"}, "resolvable": true}]},
				{"id": "o1", "notes": [{"id": 2, "body": "old remark", "author": {"name": "Ada"}, "resolvable": true,
					"position": {"position_type": "text", "new_path": "a.go", "new_line": 9, "head_sha": "older"}}]}]`))
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`{"iid": 7, "title": "T", "state": "opened", "diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}}`))
		}
	}))
	defer srv.Close()
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	link := srv.URL + "/g/p/-/merge_requests/7"
	c, r, _ := m.gitlabMR(link)
	out, _ := m.Update(m.showMR(c, r, link, "T")())
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "2 open · 0 resolved") {
		t.Errorf("panel: no thread counts:\n%s", view)
	}
	out, cmd := m.handleKey(keyMsg(t, "d"))
	out, _ = out.(Model).Update(cmd())
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Discussions", "Looks good overall", "outdated · a.go:9", "old remark"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	d := m.diff
	for i, row := range d.rows { // the cursor onto the general thread
		if row.kind == diffRowNote && row.noteHead && d.threads[row.thread].ID == "g1" {
			d.setPos(d.visPos[i])
		}
	}
	out, _ = m.handleKey(keyMsg(t, "c"))
	m = out.(Model)
	if !m.diffNoteActive() || m.diff.note.replyTo != "g1" {
		t.Fatalf("c: not a reply to g1: %+v", m.diff.note)
	}
	m.diff.note.input.SetValue("Thanks")
	out, cmd = m.handleKey(keyMsg(t, "enter"))
	out, _ = out.(Model).Update(cmd())
	m = out.(Model)
	out, cmd = m.handleKey(keyMsg(t, "R"))
	out.(Model).Update(cmd())
	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 2 || !strings.HasPrefix(writes[0], "POST /api/v4/projects/g/p/merge_requests/7/discussions/g1/notes") || !strings.Contains(writes[0], "Thanks") ||
		!strings.HasPrefix(writes[1], "PUT /api/v4/projects/g/p/merge_requests/7/discussions/g1") || !strings.Contains(writes[1], `"resolved":true`) {
		t.Errorf("writes:\n%s", strings.Join(writes, "\n"))
	}
}
