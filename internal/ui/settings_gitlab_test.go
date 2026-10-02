package ui

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestSettingsGitLab: settings signs in to each GitLab instance and lists
// it under GitLab, its line on the selected row; enter says where to set it.
func TestSettingsGitLab(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"username": "ada", "name": "Ada Lovelace"}`))
	}))
	defer srv.Close()
	host := forge.HostOf(srv.URL)
	m := jiraTabModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	out, cmd := m.handleKey(keyMsg(t, ","))
	if cmd == nil {
		t.Fatal("settings did not check GitLab")
	}
	out, _ = out.(Model).Update(cmd())
	m = out.(Model)
	s := m.settings
	s.idx = slices.IndexFunc(s.rows, func(r settingRow) bool { return r.name == host })
	if s.idx < 0 {
		t.Fatalf("no row for %s: %+v", host, s.rows)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"GitLab", "ada (config)", "signed in as Ada Lovelace (ada), token from config"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	out, _ = m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); m.settings.input != nil || !strings.Contains(m.settings.err, "gitlab:") {
		t.Errorf("enter on an instance: input %v, err %q", m.settings.input != nil, m.settings.err)
	}
}
