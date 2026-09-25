package ui

import (
	"strings"
	"testing"
)

// TestSiteSwitch: @ lists the sites; picking another ends the app with it.
func TestSiteSwitch(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "@"))
	if m = out.(Model); m.jiraPicker.active || !strings.Contains(m.status, "one Jira site") {
		t.Fatal("one site: nothing to switch to")
	}
	m = m.WithSites([]string{"", "work"}, "")
	out, _ = m.handleJiraKey(keyMsg(t, "@"))
	m = out.(Model)
	if !m.jiraPicker.active || len(m.jiraPicker.items) != 2 || !m.jiraPicker.items[0].current {
		t.Fatalf("picker = %+v", m.jiraPicker)
	}
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if next, ok := m.NextSite(); !ok || next != "work" || cmd == nil {
		t.Errorf("next %q %v", next, ok)
	}
}
