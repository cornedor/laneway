package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestTimeMachine: ctrl+t reads the changelogs; ← replays the lanes a day
// back (a card made since drops out), → returns, and keys that write wait.
func TestTimeMachine(t *testing.T) {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	moved := midnight.Add(now.Sub(midnight) / 2).Format("2006-01-02T15:04:05.000-0700")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			io.WriteString(w, `[]`)
			return
		}
		var body struct {
			JQL, Expand string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.JQL, "ABC-2") || body.Expand != "changelog" {
			t.Errorf("search %+v", body)
		}
		io.WriteString(w, `{"issues":[{"key":"ABC-2","fields":{},"changelog":{"histories":[
			{"created":"`+moved+`","items":[{"field":"status","from":"1","to":"3"}]}]}}]}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.jiraTab.cards[2].Created = midnight.Add(now.Sub(midnight) / 3) // ABC-3, made today

	out, cmd := m.handleJiraKey(keyMsg(t, "ctrl+t"))
	m = out.(Model)
	out, _ = m.handleTimeMachine(cmd().(timeMachineMsg))
	m = out.(Model)
	lanes := func() string {
		var s []string
		for _, l := range m.jiraTab.lanes {
			var ks []string
			for _, ci := range l.cards {
				ks = append(ks, m.jiraTab.cards[ci].Key)
			}
			s = append(s, strings.Join(ks, ","))
		}
		return strings.Join(s, " | ")
	}
	if got := lanes(); got != "ABC-1,ABC-2 |  | ABC-4" {
		t.Errorf("yesterday: %q", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "as of ") {
		t.Error("header lacks the date")
	}
	out, _ = m.handleJiraKey(keyMsg(t, "H"))
	if m = out.(Model); !strings.Contains(m.status, "only looks") {
		t.Errorf("H in the past: %q", m.status)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "right"))
	m = out.(Model)
	if m.jiraTab.past != nil || lanes() != "ABC-1,ABC-3 | ABC-2 | ABC-4" {
		t.Errorf("back to now: past %v, lanes %q", m.jiraTab.past, lanes())
	}
}
