package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestInboxAcrossSites: the inbox lists the other sites' threads too,
// tagged, and enter on one opens it in the browser.
func TestInboxAcrossSites(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraClient = (&fakeInbox{issues: []string{"ABC-1 S"}, updated: time.Now()}).client(t)
	club := (&fakeInbox{issues: []string{"CLB-7 S"}, updated: time.Now(), age: map[string]time.Duration{"CLB-7": time.Hour}}).client(t)
	m = m.WithSites([]string{"", "club"}, "").WithSiteClients(func(site string) (*jira.Client, error) { return club, nil })
	out, cmd := m.handleJiraKey(keyMsg(t, "I"))
	m = syncInbox(t, out.(Model), cmd)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "ABC-1 S") || !strings.Contains(view, "[club] CLB-7 S") || !strings.Contains(view, "Inbox 2") {
		t.Fatalf("inbox:\n%s", view)
	}
	if m.inboxUnread != 1 {
		t.Errorf("badge %d, want 1: ABC-1 is shown", m.inboxUnread)
	}
	m = inboxKey(t, m, "j")
	out, cmd = m.handleInboxKey(keyMsg(t, "enter"))
	if m = out.(Model); cmd == nil || !strings.Contains(m.status, "opening http") || m.refOpen {
		t.Errorf("another site's thread: status %q, panel %v", m.status, m.refOpen)
	}
}
