package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestReleases: V lists the versions with their progress, enter shows one's
// issues as a view, and its release row releases it on a second enter.
func TestReleases(t *testing.T) {
	var released string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/project/ABC/version":
			io.WriteString(w, `{"values":[
				{"id":"11","name":"1.1","issuesStatusForFixVersion":{"toDo":2,"done":2}},
				{"id":"10","name":"1.0","released":true,"releaseDate":"2026-09-01","issuesStatusForFixVersion":{"done":3}},
				{"id":"9","name":"0.9","released":true,"archived":true}
			],"isLast":true}`)
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/3/version/11":
			b, _ := io.ReadAll(r.Body)
			released = string(b)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "V"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickReleases {
		t.Fatal("V should open the releases")
	}
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"1.1  ████░░░░ 2/4 done  · unreleased", "↳ release 1.1 today (2 not done)", "1.0  ████████ 3/3 done  · released 2026-09-01"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "0.9") {
		t.Error("an archived version should be left out")
	}

	m.jiraPicker.idx = 1 // the release row
	out, cmd = m.applyJiraPick()
	if m = out.(Model); cmd != nil || !m.jiraPicker.active {
		t.Fatal("the first enter should only ask")
	}
	out, cmd = m.applyJiraPick()
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the second enter should release")
	}
	out, _ = m.Update(cmd())
	if m = out.(Model); !strings.Contains(released, `"released":true`) || m.status != "1.1 released" {
		t.Errorf("release body %q, status %q", released, m.status)
	}

	m.openReleases()
	m.jiraPicker.items = releaseItems([]jira.Version{{ID: "11", Name: "1.1"}})
	m.jiraPicker.idx = 0
	out, _ = m.applyJiraPick()
	m = out.(Model)
	if v := m.jiraTab.views[len(m.jiraTab.views)-1]; v.name != "Release: 1.1" || v.jql != "fixVersion = 11 ORDER BY status, rank" {
		t.Errorf("view = %q %q", v.name, v.jql)
	}
}
