package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

func TestStandupText(t *testing.T) {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	got := standupText([]jira.InboxEntry{
		{Key: "A-1", Summary: "Login", When: yesterday, What: "status: To Do → In progress"},
		{Key: "A-1", Summary: "Login", When: now, What: "status: In progress → Done"},
		{Key: "A-2", Summary: "Docs", When: now, What: "logged 1h"},
		{Key: "A-1", Summary: "Login", When: now, What: "commented: shipped"},
	})
	want := "Yesterday\n- A-1 Login: status: To Do → In progress\n\nToday\n" +
		"- A-1 Login: status: In progress → Done; commented: shipped\n- A-2 Docs: logged 1h"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestStandupCopy: the first row copies the text.
func TestStandupCopy(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "U"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickStandup {
		t.Fatal("U should open the standup")
	}
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickStandup,
		items: []jiraPickerItem{{id: "copy", label: "Copy as text"}, {label: "── Today"}}, text: "Today\n- A-1"})
	m = out.(Model)
	out, cmd := m.applyJiraPick()
	if m = out.(Model); cmd == nil || m.jiraPicker.active || !strings.Contains(m.status, "copied") {
		t.Errorf("copy: cmd %v, status %q", cmd != nil, m.status)
	}
}

// TestHistoryKey: H in the panel opens the issue's history.
func TestHistoryKey(t *testing.T) {
	m := loadedJiraModel(t)
	out, cmd := m.handleRefKey(keyMsg(t, "H"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickHistory || cmd == nil || !strings.Contains(m.jiraPicker.title, "ABC-1") {
		t.Fatalf("picker = %+v", m.jiraPicker)
	}
}

// TestDevInfoKey: D lists the issue's development items; enter opens one.
func TestDevInfoKey(t *testing.T) {
	m := loadedJiraModel(t)
	out, cmd := m.handleRefKey(keyMsg(t, "D"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickDev || cmd == nil {
		t.Fatalf("picker = %+v", m.jiraPicker)
	}
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickDev,
		items: []jiraPickerItem{{id: "https://g/2", label: "OPEN     Fix login"}}})
	m = out.(Model)
	out, cmd = m.applyJiraPick()
	if m = out.(Model); cmd == nil || !strings.Contains(m.status, "opening https://g/2") {
		t.Errorf("status %q", m.status)
	}
}
