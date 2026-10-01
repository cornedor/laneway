package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// TestStandupCommits: a commit joins its issue's line in the text, keyless
// ones a no-ticket line after the issues.
func TestStandupCommits(t *testing.T) {
	now := time.Now()
	entries := work.WithCommits(
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
