package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestGitCommits: your commits since the day, keyed by their subject;
// others' and older ones left out.
func TestGitCommits(t *testing.T) {
	repo := t.TempDir()
	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(cmd.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skip("git:", err, string(out))
		}
	}
	git(nil, "init", "-q")
	git(nil, "config", "user.email", "me@x.test")
	git(nil, "config", "user.name", "Me")
	old := []string{"GIT_AUTHOR_DATE=2020-01-01T10:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T10:00:00Z"}
	git(old, "commit", "-q", "--allow-empty", "-m", "ABC-1 long ago")
	git(nil, "commit", "-q", "--allow-empty", "-m", "Fix login ABC-2")
	git(nil, "commit", "-q", "--allow-empty", "-m", "utf-8 names in paths")
	git([]string{"GIT_AUTHOR_EMAIL=bob@x.test"}, "commit", "-q", "--allow-empty", "-m", "ABC-3 bob's")
	got := gitCommits([]string{repo, repo, t.TempDir()}, time.Now().Add(-time.Hour))
	var rows []string
	for _, e := range got {
		rows = append(rows, e.Key+"|"+e.What)
	}
	if strings.Join(rows, "\n") != "|commit: utf-8 names in paths\nABC-2|commit: Fix login ABC-2" {
		t.Errorf("commits:\n%s", strings.Join(rows, "\n"))
	}
}

// TestStandupCommits: a commit joins its issue's line in the text, keyless
// ones a no-ticket line after the issues.
func TestStandupCommits(t *testing.T) {
	now := time.Now()
	entries := withCommits(
		[]jira.InboxEntry{{Key: "ABC-2", Summary: "Login", When: now.Add(-2 * time.Minute), What: "status: To Do → Done"}},
		[]jira.InboxEntry{{When: now.Add(-3 * time.Minute), What: "commit: tidy"}, {Key: "ABC-2", When: now.Add(-time.Minute), What: "commit: fix"}})
	if want := "Today\n- ABC-2 Login: status: To Do → Done; commit: fix\n- no ticket: commit: tidy"; standupText(entries) != want {
		t.Errorf("text:\n%s\nwant:\n%s", standupText(entries), want)
	}
}

// TestTeamStandupRows: U opens the standup in place of the board; tab
// walks the board with what was done on each card, p groups it by person,
// tab again is yours; esc goes back to the board.
func TestTeamStandupRows(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"a1"}`)
		case "/rest/api/3/search/jql":
			io.WriteString(w, `{"issues":[{"key":"ABC-1","fields":{"summary":"First"}}]}`)
		case "/rest/api/3/issue/ABC-1/worklog":
			io.WriteString(w, `{"worklogs":[{"author":{"accountId":"a1","displayName":"Ada"},"started":"`+
				now.Add(-time.Minute).Format("2006-01-02T15:04:05.000-0700")+`","timeSpentSeconds":7200}]}`)
		case "/rest/api/3/issue/ABC-1/changelog":
			io.WriteString(w, `{"total":0,"values":[]}`)
		default:
			io.WriteString(w, `{"comments":[]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.jiraTab.cards = append(m.jiraTab.cards, jira.Card{Key: "ABC-5", Assignee: "Bo", AssigneeID: "b2"})
	press := func(k tea.KeyPressMsg) {
		t.Helper()
		out, cmd := m.handleKey(k)
		m = out.(Model)
		if msg, ok := findMsg[standupMsg](cmd); ok {
			out, _ = m.Update(msg)
			m = out.(Model)
		}
	}
	press(keyStr("U"))
	if m.jiraTab.standup == nil || m.jiraTab.standup.team {
		t.Fatal("U did not open your standup")
	}
	press(keyMsg(t, "tab"))
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Team standup · since", "To do (1)", "ABC-1 First", "Ada", "logged 2h"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	if s := m.jiraTab.standup; s.text != "To do\n- ABC-1 First · Ada · logged 2h" || s.lines[s.row].key != "ABC-1" {
		t.Errorf("text %q, row %d", s.text, s.row)
	}
	press(keyStr("p"))
	if s := m.jiraTab.standup; !s.byPerson || !strings.HasPrefix(s.text, "Ada · logged 2h\n- ABC-1 First · To do · logged 2h") {
		t.Errorf("by person: %v %q", s.byPerson, s.text)
	}
	press(keyMsg(t, "tab"))
	if m.jiraTab.standup.team {
		t.Error("tab again should be yours")
	}
	press(keyStr("y"))
	press(keyStr("esc"))
	if m.jiraTab.standup != nil {
		t.Error("esc left the standup open")
	}
}
