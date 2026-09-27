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

// inboxSite is a fake Jira whose inbox holds one change on key by who.
func inboxSite(t *testing.T, key, who string) *jira.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case r.URL.Path == "/rest/api/3/search/jql":
			io.WriteString(w, `{"issues":[{"key":"`+key+`","fields":{"summary":"S"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/changelog"):
			io.WriteString(w, `{"total":1,"values":[{"author":{"accountId":"x","displayName":"`+who+`"},
			  "created":"`+time.Now().Add(-time.Minute).Format("2006-01-02T15:04:05.000-0700")+`","items":[{"field":"status","fromString":"A","toString":"B"}]}]}`)
		default:
			io.WriteString(w, `{"comments":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
}

// TestInboxAcrossSites: the inbox lists the other sites' changes too,
// tagged, and enter on one opens it in the browser.
func TestInboxAcrossSites(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraClient = inboxSite(t, "ABC-1", "Ann")
	club := inboxSite(t, "CLB-7", "Bob")
	m = m.WithSites([]string{"", "club"}, "").WithSiteClients(func(site string) (*jira.Client, error) { return club, nil })
	out, cmd := m.handleJiraKey(keyMsg(t, "I"))
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Ann  ABC-1 S") || !strings.Contains(view, "Bob  [club] CLB-7 S") || !strings.Contains(view, "2 changes on 2 issues") {
		t.Fatalf("inbox:\n%s", view)
	}
	if n := m.countInbox()().(inboxCountMsg).n; n != 2 {
		t.Errorf("badge %d, want 2", n)
	}
}
