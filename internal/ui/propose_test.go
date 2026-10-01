package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// TestProposals: g in the timesheet reads git and ui.activity, lists the
// proposals, and enter on one opens the worklog input with its time.
func TestProposals(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "-q", repo}, {"-C", repo, "config", "user.email", "me@x.test"}, {"-C", repo, "config", "user.name", "Me"},
		{"-C", repo, "commit", "-q", "--allow-empty", "-m", "ABC-2 first"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	day := time.Now().AddDate(0, 0, -1) // today's commit stays out
	nine := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.Local)
	script := filepath.Join(dir, "activity.sh")
	lines := ""
	for _, m := range []int{0, 20, 50} {
		lines += nine.Add(time.Duration(m)*time.Minute).Format(time.RFC3339) + "\tworked on ABC-5\\n"
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '"+lines+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		default:
			io.WriteString(w, `{"issues":[]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.jiraRepos = map[string]string{"ABC": repo}
	m.uiConfig.Activity = []string{script, filepath.Join(dir, "missing")}
	ics := filepath.Join(dir, "cal.ics")
	two := nine.Add(5 * time.Hour).UTC().Format("20060102T150405Z")
	half := nine.Add(5*time.Hour + 30*time.Minute).UTC().Format("20060102T150405Z")
	if err := os.WriteFile(ics, []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTAMP:"+two+"\r\nDTSTART:"+two+
		"\r\nDTEND:"+half+"\r\nSUMMARY:Planning\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.uiConfig.Calendar, m.uiConfig.MeetingKey = ics, "OPS-1"
	out, _ := m.handleJiraKey(keyMsg(t, "W"))
	m = out.(Model)
	out, _ = m.handleJiraPickerKey(keyMsg(t, "["))
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickTimesheet,
		items: []jiraPickerItem{{label: "nothing logged"}}})
	m = out.(Model)
	out, cmd := m.handleJiraPickerKey(keyMsg(t, "p"))
	m = out.(Model)
	out, _ = m.handleProposals(cmd().(proposalsMsg))
	m = out.(Model)
	var labels []string
	for _, it := range m.jiraPicker.items {
		labels = append(labels, it.label)
	}
	all := strings.Join(labels, "\n")
	if !strings.Contains(all, "≈ 08:45      1h  ABC-5 — 3 activity.sh") || !strings.Contains(all, "≈ 14:00     30m  OPS-1 — Planning") || strings.Contains(all, "ABC-2") {
		t.Fatalf("rows:\n%s", all)
	}
	if !strings.Contains(m.status, "ui.activity failed: "+filepath.Join(dir, "missing")) { // the command as configured
		t.Errorf("status %q", m.status)
	}
	for i, it := range m.jiraPicker.items {
		if strings.Contains(it.label, "ABC-5") {
			m.jiraPicker.idx = i
		}
	}
	out, _ = m.applyJiraPick()
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "worklog" || m.jiraFieldKey != "ABC-5" || !strings.HasPrefix(m.jiraFieldInput.Value(), "1h") ||
		!m.worklogStart.Equal(nine.Add(-15*time.Minute).Truncate(time.Second)) {
		t.Errorf("input %q %q %q, start %v", m.jiraFieldName, m.jiraFieldKey, m.jiraFieldInput.Value(), m.worklogStart)
	}
}
