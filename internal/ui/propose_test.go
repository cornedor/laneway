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

// TestProposeWork: events close together are one session, the time up to
// an event goes to its issue, a session's first event counts a quarter,
// and what is logged comes off.
func TestProposeWork(t *testing.T) {
	at := func(hm string) time.Time {
		v, _ := time.ParseInLocation("15:04", hm, time.Local)
		return v
	}
	events := []workEvent{
		{at: at("09:00"), key: "A-1", source: "commit"},
		{at: at("09:20"), key: "A-1", source: "claude"},
		{at: at("09:50"), key: "A-2", source: "git"},    // 30m: still the session
		{at: at("11:00"), key: "A-2", source: "commit"}, // after a gap: a quarter
		{at: at("11:05"), key: "", source: "claude"},    // no key: gone
		{at: at("14:00"), key: "A-3", source: "commit"}, // logged already
	}
	got := proposeWork(events, map[string]int{"A-3": 3600})
	if len(got) != 2 {
		t.Fatalf("proposals %+v", got)
	}
	if p := got[0]; p.key != "A-1" || p.seconds != 30*60 || !p.start.Equal(at("08:45")) || p.sourcesText() != "1 claude, 1 commit" {
		t.Errorf("A-1: %+v", p)
	}
	if p := got[1]; p.key != "A-2" || p.seconds != 45*60 {
		t.Errorf("A-2: %d, want 45m", p.seconds)
	}
}

func TestReflogWork(t *testing.T) {
	log := "100\tcheckout: moving from main to issue/ABC-3-login\n200\tcommit: wip\n300\tcheckout: moving from issue/ABC-3-login to main\n400\tcommit: ABC-9 tidy\n"
	var keys []string
	for _, e := range reflogWork(log, time.Unix(1000, 0)) {
		keys = append(keys, e.key)
	}
	if strings.Join(keys, " ") != "ABC-3 ABC-3  ABC-9" {
		t.Errorf("keys %q", keys)
	}
}

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
	if !strings.Contains(all, "≈ 08:45      1h  ABC-5 — 3 activity.sh") || strings.Contains(all, "ABC-2") {
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
		!m.worklogStart.Equal(nine.Add(-proposeLead).Truncate(time.Second)) {
		t.Errorf("input %q %q %q, start %v", m.jiraFieldName, m.jiraFieldKey, m.jiraFieldInput.Value(), m.worklogStart)
	}
}
