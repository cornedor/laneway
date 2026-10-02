package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestMRView: enter on a GitLab merge request in D reads it in the panel
// (state, pipeline by stage, approvals, description); esc goes back to the
// issue; a link no instance has stays a browser link.
func TestMRView(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/approvals"):
			w.Write([]byte(`{"approvals_required": 2, "approvals_left": 1, "approved_by": [{"user": {"name": "Grace"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/jobs"):
			w.Write([]byte(`[{"name": "unit", "stage": "test", "status": "failed"}]`))
		default:
			w.Write([]byte(`{"iid": 7, "project_id": 3, "title": "Fix login", "state": "opened", "source_branch": "issue/ABC-1", "target_branch": "main",
				"detailed_merge_status": "mergeable", "description": "Fixes **it**.", "author": {"name": "Ada"}, "head_pipeline": {"id": 9, "status": "failed"}}`))
		}
	}))
	defer srv.Close()
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	out, _ := m.handleRefKey(keyMsg(t, "D"))
	m = out.(Model)
	link := srv.URL + "/g/p/-/merge_requests/7"
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickDev,
		items: []jiraPickerItem{{id: link, label: "OPEN     Fix login"}, {id: "https://elsewhere.test/pr/1", label: "OPEN     Other"}}})
	m = out.(Model)
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if m.mr == nil || cmd == nil {
		t.Fatalf("no merge request view; status %q", m.status)
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Fix login", "GitLab g/p!7", "issue/ABC-1 → main", "1 of 2", "Grace", "✗ failed", "test", "✗ unit", "Fixes it."} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	out, _ = m.handleRefKey(keyMsg(t, "esc"))
	if m = out.(Model); m.mr != nil {
		t.Error("esc kept the merge request")
	}
	out, _ = m.handleRefKey(keyMsg(t, "D"))
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickDev,
		items: []jiraPickerItem{{id: "https://elsewhere.test/pr/1", label: "OPEN     Other"}}})
	out, _ = out.(Model).applyJiraPick()
	if m = out.(Model); m.mr != nil || !strings.Contains(m.status, "opening https://elsewhere.test/pr/1") {
		t.Errorf("another forge's link: mr %v, status %q", m.mr != nil, m.status)
	}
}
