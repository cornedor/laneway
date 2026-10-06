package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// fakeMRGitLab is a GitLab with !7 on g/p, its writes kept by path.
func fakeMRGitLab(t *testing.T, mr string) (*httptest.Server, map[string]map[string]any) {
	t.Helper()
	wrote := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			wrote[r.URL.Path] = b
			w.Write([]byte(`{}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/members/all"):
			w.Write([]byte(`[{"id": 1, "username": "ada", "name": "Ada"}, {"id": 2, "username": "grace", "name": "Grace"}]`))
		case strings.HasSuffix(r.URL.Path, "/labels"):
			w.Write([]byte(`[{"name": "bug"}, {"name": "ui"}]`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests/7"):
			w.Write([]byte(mr))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, wrote
}

// mrInPanel opens link's merge request in the panel from D, as enter does.
func mrInPanel(t *testing.T, m Model, link string) Model {
	t.Helper()
	out, _ := m.handleRefKey(keyMsg(t, "D"))
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickDev,
		items: []jiraPickerItem{{id: link, label: "OPEN     Fix login"}}})
	out, cmd := out.(Model).applyJiraPick()
	if m = out.(Model); m.mr == nil {
		t.Fatalf("no merge request in the panel; status %q", m.status)
	}
	if cmd != nil {
		out, _ = m.Update(cmd())
		m = out.(Model)
	}
	return m
}

// run runs cmd's message through m; after a write, the read that follows
// it too.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatalf("no command; status %q", m.status)
	}
	msg := cmd()
	out, next := m.Update(msg)
	if _, ok := msg.(mrWroteMsg); ok && next != nil {
		out, _ = out.(Model).Update(next())
	}
	return out.(Model)
}

// TestMREdit: e lists what changes; reviewers tick among the members, enter
// on one adds them to who was there; the draft row flips the title's
// prefix; a label is ticked off.
func TestMREdit(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir())
	srv, wrote := fakeMRGitLab(t, `{"iid": 7, "title": "Fix login", "state": "opened", "source_branch": "fix", "target_branch": "main",
		"reviewers": [{"id": 1, "name": "Ada"}], "labels": ["bug"], "detailed_merge_status": "mergeable"}`)
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	m = mrInPanel(t, m, srv.URL+"/g/p/-/merge_requests/7")
	path := "/api/v4/projects/g/p/merge_requests/7"

	out, _ := m.handleRefKey(keyMsg(t, "e"))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Edit g/p!7", "Reviewers  Ada", "Mark as draft", "Target branch  main"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	m.jiraPicker.idx = 2 // Reviewers
	out, cmd := m.applyJiraPick()
	m = run(t, out.(Model), cmd)
	if m.jiraPicker.kind != jiraPickMRPeople || len(m.jiraPicker.items) != 2 || !m.jiraPicker.items[0].current {
		t.Fatalf("reviewers picker: %+v", m.jiraPicker.items)
	}
	m.jiraPicker.idx = 1 // Grace
	out, cmd = m.handleKey(keyMsg(t, "enter"))
	m = run(t, out.(Model), cmd)
	if got, _ := json.Marshal(wrote[path]["reviewer_ids"]); string(got) != "[1,2]" {
		t.Errorf("reviewer_ids = %s, want [1,2]", got)
	}

	out, _ = m.handleRefKey(keyMsg(t, "e"))
	m = out.(Model)
	m.jiraPicker.idx = 1 // Mark as draft
	out, cmd = m.applyJiraPick()
	m = run(t, out.(Model), cmd)
	if wrote[path]["title"] != "Draft: Fix login" {
		t.Errorf("draft: %v", wrote[path])
	}

	out, _ = m.handleRefKey(keyMsg(t, "e"))
	m = out.(Model)
	m.jiraPicker.idx = 4 // Labels
	out, cmd = m.applyJiraPick()
	m = run(t, out.(Model), cmd)
	m.jiraPicker.idx = 0 // bug, ticked: enter unticks it
	out, cmd = m.handleKey(keyMsg(t, "enter"))
	run(t, out.(Model), cmd)
	if wrote[path]["labels"] != "" {
		t.Errorf("labels: %v", wrote[path])
	}
}

// TestMRMerge: M lists the ways to merge, GitLab's defaults first; enter
// merges with them. One not ready says why, in the panel and the diff.
func TestMRMerge(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir())
	srv, wrote := fakeMRGitLab(t, `{"iid": 7, "title": "Fix login", "state": "opened", "source_branch": "fix", "target_branch": "main",
		"detailed_merge_status": "mergeable", "squash": true}`)
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	m = mrInPanel(t, m, srv.URL+"/g/p/-/merge_requests/7")
	out, _ := m.handleRefKey(keyMsg(t, "M"))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Merge fix into main?", "Squashed, keep the branch  GitLab's default"} {
		if !strings.Contains(view, want) {
			t.Fatalf("no %q:\n%s", want, view)
		}
	}
	out, cmd := m.handleKey(keyMsg(t, "enter"))
	m = run(t, out.(Model), cmd)
	if b := wrote["/api/v4/projects/g/p/merge_requests/7/merge"]; b["squash"] != true || b["should_remove_source_branch"] != false {
		t.Errorf("merge = %v", b)
	}
	if m.status != "merged g/p!7" {
		t.Errorf("status %q", m.status)
	}

	m.mr.mr.Mergeable, m.mr.mr.MergeStatus = false, "pipeline still running"
	out, _ = m.handleRefKey(keyMsg(t, "M"))
	if m = out.(Model); m.jiraPicker.active || m.status != "not ready to merge: pipeline still running" {
		t.Errorf("not ready: picker %v, status %q", m.jiraPicker.active, m.status)
	}
	m.diff = &diffState{label: "g/p!7"}
	out, _ = m.handleDiffKey(keyMsg(t, "M"))
	if m = out.(Model); m.diff.merges != nil {
		t.Error("the diff's M on one not ready")
	}
	m.mr.mr.Mergeable = true
	out, _ = m.handleDiffKey(keyMsg(t, "M"))
	if m = out.(Model); len(m.diff.merges) != 4 {
		t.Fatalf("the diff's M: %+v", m.diff.merges)
	}
	out, cmd = m.handleDiffKey(keyMsg(t, "enter"))
	if m = out.(Model); m.diff.merges != nil || cmd == nil {
		t.Error("enter in the diff's M merges")
	}
}

// TestMRSignIn: a merge request on a host with no token says how to sign
// in; r after glab auth login reads it.
func TestMRSignIn(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir())
	srv, _ := fakeMRGitLab(t, `{"iid": 7, "title": "Fix login", "state": "opened"}`)
	sites := gitlab.NewSites(nil)
	m := loadedJiraModel(t).WithGitLab(sites)
	m = mrInPanel(t, m, srv.URL+"/g/p/-/merge_requests/7")
	host := strings.TrimPrefix(srv.URL, "http://")
	view := ansi.Strip(m.renderMR(300))
	for _, want := range []string{"No GitLab token for " + host, "glab auth login --hostname " + host, "personal_access_tokens", "r try again"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	out, cmd := m.handleRefKey(keyMsg(t, "r"))
	if m = out.(Model); cmd != nil || !strings.Contains(m.status, "still no GitLab token") {
		t.Errorf("r with no token yet: status %q", m.status)
	}
}
