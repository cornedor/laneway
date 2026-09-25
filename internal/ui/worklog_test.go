package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// worklogJira records worklog posts on the model's client.
func worklogJira(t *testing.T, m *Model, bodies *[]map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.Method == http.MethodGet {
			return // the reload after a delete
		}
		b["path"] = r.URL.Path
		*bodies = append(*bodies, b)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
}

// TestLogWork: w in the panel, "1h 30m tests" logs 90 minutes ending now.
func TestLogWork(t *testing.T) {
	var bodies []map[string]any
	m := loadedJiraModel(t)
	worklogJira(t, &m, &bodies)
	out, _ := m.handleRefKey(keyMsg(t, "w"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "worklog" || !strings.Contains(m.View().Content, "Log work — ABC-1") {
		t.Fatal("w should open the worklog input")
	}
	m.jiraFieldInput.SetValue("tests")
	out, cmd := m.applyJiraField()
	if cmd != nil || !out.(Model).jiraFieldActive || !strings.Contains(out.(Model).status, "start with a time") {
		t.Fatal("no time should be refused")
	}
	m.jiraFieldInput.SetValue("1h 30m tests")
	_, cmd = m.applyJiraField()
	if msg := cmd().(jiraMutatedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	b := bodies[0]
	started, _ := time.Parse("2006-01-02T15:04:05.000-0700", b["started"].(string))
	if b["path"] != "/rest/api/3/issue/ABC-1/worklog" || b["timeSpentSeconds"] != 5400.0 || b["comment"] == nil ||
		time.Since(started) < 89*time.Minute || time.Since(started) > 91*time.Minute {
		t.Errorf("body = %v", b)
	}
}

// TestTimer: T starts a timer the header shows and the store keeps; T again
// stops it into the log input, started when the timer did.
func TestTimer(t *testing.T) {
	var bodies []map[string]any
	m := jiraTabModel(t)
	worklogJira(t, &m, &bodies)
	out, cmd := m.handleJiraKey(keyMsg(t, "T"))
	m = out.(Model)
	if m.timer.key != "ABC-1" || cmd == nil {
		t.Fatalf("timer = %+v", m.timer)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "⏱ ABC-1 0m") {
		t.Error("header lacks the timer")
	}
	// A restart picks it up.
	m.timer = workTimer{}
	m.loadTimer()
	if m.timer.key != "ABC-1" {
		t.Fatal("timer not restored from the store")
	}
	m.timer.start = time.Now().Add(-25 * time.Minute)
	start := m.timer.start
	out, _ = m.handleJiraKey(keyMsg(t, "T"))
	m = out.(Model)
	if m.timer.key != "" || !m.jiraFieldActive || m.jiraFieldInput.Value() != "25m " || m.jiraFieldKey != "ABC-1" {
		t.Fatalf("stop: timer %+v, input %q", m.timer, m.jiraFieldInput.Value())
	}
	if v, _, _ := m.store.GetMeta(timerMeta); v != "" {
		t.Errorf("stored timer = %q after stop", v)
	}
	_, cmd = m.applyJiraField()
	cmd()
	if got := bodies[0]["started"]; got != start.Format("2006-01-02T15:04:05.000-0700") {
		t.Errorf("started = %v, want the timer's start", got)
	}
}

func TestTimesheet(t *testing.T) {
	m := jiraTabModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "W"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickTimesheet || cmd == nil {
		t.Fatal("W should open the timesheet")
	}
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickTimesheet,
		title: "Today — 1h 30m", items: []jiraPickerItem{{id: "ABC-2", label: "09:00  1h 30m  ABC-2 Second"}}})
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Today — 1h 30m") || !strings.Contains(view, "ABC-2 Second") {
		t.Error("timesheet not drawn")
	}
	out, _ = m.applyJiraPick()
	if m = out.(Model); !m.refOpen || m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Error("enter should open the issue")
	}
}

// TestTimesheetDays: [ ] step the day; d twice deletes the entry.
func TestTimesheetDays(t *testing.T) {
	var bodies []map[string]any
	m := jiraTabModel(t)
	worklogJira(t, &m, &bodies)
	out, _ := m.handleJiraKey(keyMsg(t, "W"))
	m = out.(Model)
	today := m.jiraPicker.day
	out, _ = m.handleJiraPickerKey(keyMsg(t, "["))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.day.Format(time.DateOnly) != today.AddDate(0, 0, -1).Format(time.DateOnly) || m.jiraPicker.title != "Yesterday" {
		t.Fatalf("day %v, title %q", m.jiraPicker.day, m.jiraPicker.title)
	}
	m.setJiraPickerItems([]jiraPickerItem{{id: "ABC-2/10101", label: "09:00  1h  ABC-2"}})
	out, cmd := m.handleJiraPickerKey(keyMsg(t, "d"))
	m = out.(Model)
	if cmd != nil || !strings.Contains(m.status, "d again") {
		t.Fatalf("first d: status %q", m.status)
	}
	_, cmd = m.handleJiraPickerKey(keyMsg(t, "d"))
	if cmd == nil {
		t.Fatal("second d should delete")
	}
	cmd()
	if len(bodies) == 0 || bodies[0]["path"] != "/rest/api/3/issue/ABC-2/worklog/10101" {
		t.Errorf("requests = %v", bodies)
	}
}

// TestTimerOnStart: with ui.timer_on_start, S's success starts the timer.
func TestTimerOnStart(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraWork(jiraWorkMsg{key: "ABC-1", path: "/w"})
	if m = out.(Model); m.timer.key != "" {
		t.Fatal("off by default")
	}
	m.opts.timerOnStart = true
	out, cmd := m.handleJiraWork(jiraWorkMsg{key: "ABC-1", path: "/w"})
	if m = out.(Model); m.timer.key != "ABC-1" || cmd == nil || !strings.Contains(m.status, "timer started") {
		t.Errorf("timer %+v, status %q", m.timer, m.status)
	}
}
