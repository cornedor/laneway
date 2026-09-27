package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestReview: ctrl+r turns the requests' keys in known projects into a
// view, and marks their cards ⌥.
func TestReview(t *testing.T) {
	old := reviewRequests
	t.Cleanup(func() { reviewRequests = old })
	reviewRequests = func(context.Context) ([]reviewRequest, error) {
		return []reviewRequest{
			{title: "ABC-2: fix login, UTF-8 paths"},
			{title: "Tidy", branch: "issue/abc-3-tidy"},
			{title: "ABC-2 again"},
		}, nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/project/search" {
			io.WriteString(w, `{"values":[{"key":"ABC","name":"Abc"}],"isLast":true}`)
			return
		}
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "ctrl+r"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("ctrl+r should ask gh and glab")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	v := m.jiraTab.views[len(m.jiraTab.views)-1]
	if v.name != "Review: waiting on me" || v.jql != "key in (ABC-2, ABC-3) ORDER BY updated DESC" {
		t.Errorf("view %q %q", v.name, v.jql)
	}
	if card := ansi.Strip(strings.Join(m.jiraLaneCard(jira.Card{Key: "ABC-2", Summary: "Second"}, false, 40), "\n")); !strings.Contains(card, "⌥") {
		t.Errorf("ABC-2's card not marked:\n%s", card)
	}
	out, _ = m.Update(reviewMsg{})
	if m = out.(Model); !strings.Contains(m.status, "nothing waits on your review") {
		t.Errorf("status %q", m.status)
	}
}
