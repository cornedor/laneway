package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestStatusTime: A's row lists each status with its time and share.
func TestStatusTime(t *testing.T) {
	created := time.Now().Add(-50 * time.Hour).UTC().Format("2006-01-02T15:04:05.000-0700")
	moved := time.Now().Add(-2 * time.Hour).UTC().Format("2006-01-02T15:04:05.000-0700")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/ABC-1":
			io.WriteString(w, `{"fields":{"created":"`+created+`","status":{"name":"Done"}}}`)
		case "/rest/api/3/issue/ABC-1/changelog":
			io.WriteString(w, `{"isLast":true,"values":[{"created":"`+moved+`","items":[{"field":"status","fromString":"To Do","toString":"Done"}]}]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, _ := m.handleJiraPickerLoaded(m.applyIssueAction("ABC-1", "status-time")().(jiraPickerLoadedMsg))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"2d 2h since created", "To Do     2d 0h  █████████░", "Done      2h 0m", "← now"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
}
