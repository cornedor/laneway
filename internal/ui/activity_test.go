package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestActivityTabs: ] and [ walk the tabs (wrapping), History loads on
// first show and draws each change; the tab stays for the next issue.
func TestActivityTabs(t *testing.T) {
	m := loadedJiraModel(t)
	if c := ansi.Strip(m.refView.GetContent()); !strings.Contains(c, "Comments (0)  History  Work log  All") || !strings.Contains(c, "no comments yet") {
		t.Fatalf("comments tab:\n%s", c)
	}
	out, cmd := m.handleRefKey(keyMsg(t, "]"))
	m = out.(Model)
	if m.activityTab != activityHistory || cmd == nil || !strings.Contains(ansi.Strip(m.refView.GetContent()), "loading…") {
		t.Fatalf("tab %d, cmd %v", m.activityTab, cmd != nil)
	}
	when := time.Date(2025, 9, 25, 9, 0, 0, 0, time.Local) // over a week back: the full date
	out, _ = m.handleActivityLoaded(activityLoadedMsg{key: "ABC-1",
		changes: []jira.InboxEntry{{Who: "Bob", When: when, What: "status: To Do → Done · labels: — → ui"}},
		logs:    []jira.Worklog{{Author: "Ann", Started: when.Add(time.Hour), Seconds: 5400, Comment: "review"}}})
	m = out.(Model)
	c := ansi.Strip(m.refView.GetContent())
	if !strings.Contains(c, "Bob · 2025-09-25 09:00") || !strings.Contains(c, "status To Do → Done") || !strings.Contains(c, "labels — → ui") {
		t.Fatalf("history:\n%s", c)
	}
	out, cmd = m.handleRefKey(keyMsg(t, "]"))
	if m = out.(Model); cmd != nil || !strings.Contains(ansi.Strip(m.refView.GetContent()), "logged 1h 30m") {
		t.Fatalf("work log: cmd %v\n%s", cmd != nil, ansi.Strip(m.refView.GetContent()))
	}
	out, _ = m.handleRefKey(keyMsg(t, "]"))
	m = out.(Model)
	c = ansi.Strip(m.refView.GetContent())
	if b, a := strings.Index(c, "Bob ·"), strings.Index(c, "Ann ·"); b < 0 || a < b {
		t.Fatalf("all, oldest first:\n%s", c)
	}
	out, _ = m.handleRefKey(keyMsg(t, "]"))
	if m = out.(Model); m.activityTab != activityComments {
		t.Fatalf("] past All: tab %d", m.activityTab)
	}
	out, _ = m.handleRefKey(keyMsg(t, "["))
	if m = out.(Model); m.activityTab != activityAll {
		t.Fatalf("[ before Comments: tab %d", m.activityTab)
	}
	// Another issue keeps the tab and fetches its own history.
	out, _ = openRefFor(m, "ABC-2")
	m = out.(Model)
	_, cmd = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-2", issue: &jira.Issue{Key: "ABC-2"}})
	if cmd == nil {
		t.Error("the next issue should load its history")
	}
}

// TestActivityClick: a click on a tab opens it.
func TestActivityClick(t *testing.T) {
	m := loadedJiraModel(t)
	out, _ := m.handleRefKey(keyMsg(t, "]"))
	m = out.(Model)
	if m.activityLine < 0 {
		t.Fatal("tab row not found")
	}
	lines := strings.Split(m.refView.GetContent(), "\n")
	y := 1 + m.crumbRows() + visualRowsBefore(lines, m.activityLine, m.refView.Width()) - m.refView.YOffset()
	listW, _ := m.jiraListWidth(m.width)
	x := listW + 1 + strings.Index(ansi.Strip(lines[m.activityLine]), "Work log") + 2
	out, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m = out.(Model); m.activityTab != activityWorklog {
		t.Errorf("tab %d after a click on Work log", m.activityTab)
	}
}

func TestActivityTabAt(t *testing.T) {
	line := " Comments (2)  History  Work log  All   [ ]"
	for col, want := range map[int]int{0: -1, 1: 0, 12: 0, 13: -1, 15: 1, 24: 2, 34: 3, 38: -1} {
		if got := activityTabAt(line, col, 2); got != want {
			t.Errorf("col %d: %d, want %d", col, got, want)
		}
	}
}

// TestCommentBylineClick: a click on a comment's byline replies to it, a
// threaded reply's too.
func TestCommentBylineClick(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	when := time.Date(2025, 9, 25, 9, 0, 0, 0, time.Local) // over a week back: the full date
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: &jira.Issue{Key: "ABC-1", Comments: []jira.Comment{
		{ID: "1", Author: "Ann", Created: when, Body: "first"},
		{ID: "2", Author: "Bob", Created: when.Add(time.Hour), Body: "second", ParentID: "1"},
	}}})
	m = out.(Model)
	lines := strings.Split(m.refView.GetContent(), "\n")
	at := -1
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "│ Bob · ") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no reply byline:\n%s", ansi.Strip(m.refView.GetContent()))
	}
	y := 1 + m.crumbRows() + visualRowsBefore(lines, at, m.refView.Width()) - m.refView.YOffset()
	listW, _ := m.jiraListWidth(m.width)
	out, _ = m.Update(tea.MouseClickMsg{X: listW + 4, Y: y, Button: tea.MouseLeft})
	if m = out.(Model); !m.jiraCommentActive || m.jiraCommentReplyTo != "Bob" {
		t.Errorf("composer %v, reply to %q", m.jiraCommentActive, m.jiraCommentReplyTo)
	}
}

// TestStatusLozenge: the panel's status is a lozenge when Jira says its
// category, plain text without one or while the field cursor is on it.
func TestStatusLozenge(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1",
		issue: &jira.Issue{Key: "ABC-1", Status: "In Progress", StatusCategory: "indeterminate"}})
	m = out.(Model)
	if c := ansi.Strip(m.refView.GetContent()); !strings.Contains(c, "Status:    IN PROGRESS ") {
		t.Fatalf("no lozenge:\n%s", c)
	}
	out, _ = m.handleRefKey(keyMsg(t, "tab")) // Summary
	m = out.(Model)
	out, _ = m.handleRefKey(keyMsg(t, "tab")) // Status
	m = out.(Model)
	if c := ansi.Strip(m.refView.GetContent()); !strings.Contains(c, "Status:   In Progress") {
		t.Errorf("selected status should read plain:\n%s", c)
	}
}

func TestRelativeDate(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{
		10 * time.Second:   "just now",
		5 * time.Minute:    "5m ago",
		3 * time.Hour:      "3h ago",
		50 * time.Hour:     "2d ago",
		8 * 24 * time.Hour: "2026-09-18 12:00",
		-2 * time.Hour:     "2026-09-26 14:00",
	} {
		if got := relativeDate(now.Add(-d), now, "2006-01-02 15:04"); got != want {
			t.Errorf("%v ago = %q, want %q", d, got, want)
		}
	}
}
