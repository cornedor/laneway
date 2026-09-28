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

// TestStandupFurther: U inside the standup reaches a workday further back.
func TestStandupFurther(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "U"))
	m = out.(Model)
	first := m.jiraPicker.day
	out, cmd := m.handleJiraPickerKey(keyMsg(t, "U"))
	m = out.(Model)
	if cmd == nil || !m.jiraPicker.day.Equal(jira.PreviousWorkday(first, nil)) || m.jiraPicker.kind != jiraPickStandup {
		t.Errorf("since %v, want %v", m.jiraPicker.day, jira.PreviousWorkday(first, nil))
	}
}

// TestStandupMine: one row per issue in sections, a stuck card shows with
// its age, the next to-dos by rank, flagged ones as blockers; the text is
// Yesterday / Today / Blockers.
func TestStandupMine(t *testing.T) {
	now := time.Now()
	since := now.AddDate(0, 0, -1)
	entries := []jira.InboxEntry{
		{Key: "A-1", Summary: "Login", When: now.Add(-3 * time.Hour), What: "status: To Do → In progress",
			Changes: []jira.Change{{Field: "status", From: "To Do", To: "In progress"}}},
		{Key: "A-1", Summary: "Login", When: now.Add(-2 * time.Hour), What: "status: In progress → Done",
			Changes: []jira.Change{{Field: "status", From: "In progress", To: "Done"}}},
		{Key: "A-1", Summary: "Login", When: now.Add(-time.Hour), What: "logged 2h", Logged: 7200},
		{Key: "A-2", Summary: "Docs", When: now, What: "commented: a"},
		{Key: "A-2", Summary: "Docs", When: now, What: "commented: b"},
		{Summary: "tidy", When: now, What: "commit: tidy"},
	}
	cards := []jira.Card{
		{Key: "A-5", Summary: "Stuck", Status: "In progress", InProgress: true, Since: now.AddDate(0, 0, -4), AssigneeID: "me", Flagged: true},
		{Key: "A-2", Summary: "Docs", Status: "In progress", InProgress: true, AssigneeID: "me"},
		{Key: "A-1", Summary: "Login", Status: "Done", Done: true, AssigneeID: "me"},
		{Key: "A-6", Summary: "Next up", Status: "To Do", AssigneeID: "me"},
		{Key: "A-7", Summary: "Theirs", Status: "To Do", AssigneeID: "ada"},
	}
	items, text := standupMine(entries, cards, "me", since, now)
	var labels []string
	for _, it := range items {
		labels = append(labels, strings.TrimSpace(it.label))
	}
	want := []string{
		"── Done since Yesterday",
		"A-1 Login · Done · To Do → Done, logged 2h",
		"── In progress",
		"A-2 Docs · In progress · 2 comments",
		"A-5 Stuck · In progress · no activity · in progress 4d",
		"── Also touched",
		"tidy · 1 commit",
		"── Next",
		"A-6 Next up · To Do",
		"── Blockers",
		"A-5 Stuck · In progress · no activity · in progress 4d",
	}
	if strings.Join(labels, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows\n%s\nwant\n%s", strings.Join(labels, "\n"), strings.Join(want, "\n"))
	}
	wantText := "Yesterday\n- A-1 Login: To Do → Done, logged 2h\n- A-2 Docs: 2 comments\n- tidy: 1 commit\n\n" +
		"Today\n- A-2 Docs\n- A-5 Stuck\n- A-6 Next up\n\nBlockers\n- A-5 Stuck"
	if text != wantText {
		t.Errorf("text\n%s\nwant\n%s", text, wantText)
	}
}
