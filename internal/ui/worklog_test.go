package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
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
	if msg := cmd().(worklogLoggedMsg); msg.err != nil || msg.fromTimer {
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
	if !m.jiraFieldActive || m.jiraFieldInput.Value() != "25m " || m.jiraFieldKey != "ABC-1" {
		t.Fatalf("stop: input %q", m.jiraFieldInput.Value())
	}
	// Until the log is in, the timer runs on: esc must not lose its time.
	if m.timer.key != "ABC-1" {
		t.Fatal("the timer should run until the log is written")
	}
	out, cmd = m.applyJiraField()
	m = out.(Model)
	msg := cmd().(worklogLoggedMsg)
	if got := bodies[0]["started"]; got != start.Format("2006-01-02T15:04:05.000-0700") {
		t.Errorf("started = %v, want the timer's start", got)
	}
	out, _ = m.handleWorklogLogged(msg)
	m = out.(Model)
	if v, _, _ := m.store.GetMeta(timerMeta); m.timer.key != "" || v != "" {
		t.Errorf("after the log: timer %+v, stored %q", m.timer, v)
	}
}

// TestTimerKeptOnFailure: a failed log leaves the timer running.
func TestTimerKeptOnFailure(t *testing.T) {
	m := jiraTabModel(t)
	m.timer = workTimer{key: "ABC-1", start: time.Now().Add(-time.Hour)}
	out, _ := m.handleWorklogLogged(worklogLoggedMsg{key: "ABC-1", fromTimer: true, err: errors.New("503")})
	if m = out.(Model); m.timer.key != "ABC-1" || !strings.Contains(m.status, "keeps running") {
		t.Errorf("timer %+v, status %q", m.timer, m.status)
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

// TestTimesheetEdit: e opens the entry's time and comment; enter updates it.
func TestTimesheetEdit(t *testing.T) {
	var bodies []map[string]any
	m := jiraTabModel(t)
	worklogJira(t, &m, &bodies)
	out, _ := m.handleJiraKey(keyMsg(t, "W"))
	m = out.(Model)
	m.setJiraPickerItems([]jiraPickerItem{{id: "ABC-2/10101", label: "09:00  1h  ABC-2", value: "1h review"}})
	out, _ = m.handleJiraPickerKey(keyMsg(t, "e"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldInput.Value() != "1h review" || m.worklogEdit != "10101" {
		t.Fatalf("input %q edit %q", m.jiraFieldInput.Value(), m.worklogEdit)
	}
	m.jiraFieldInput.SetValue("1h 30m review")
	_, cmd := m.applyJiraField()
	cmd()
	if len(bodies) == 0 || bodies[0]["path"] != "/rest/api/3/issue/ABC-2/worklog/10101" || bodies[0]["timeSpentSeconds"] != 5400.0 {
		t.Errorf("requests = %v", bodies)
	}
	m.openWorklogInput("ABC-1", "", time.Time{})
	if m.worklogEdit != "" {
		t.Error("a new log must not update the edited entry")
	}
}

// TestTimesheetCopy: y copies the day's worklogs and total as a markdown
// table.
func TestTimesheetCopy(t *testing.T) {
	now := time.Now()
	started := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 1, 0, time.Local).Format("2006-01-02T15:04:05.000-0700")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/myself"):
			io.WriteString(w, `{"accountId":"me"}`)
		case strings.HasSuffix(r.URL.Path, "/search/jql"):
			io.WriteString(w, `{"issues":[{"key":"ABC-2","fields":{"summary":"Second | part"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/worklog"):
			io.WriteString(w, `{"worklogs":[{"id":"1","author":{"accountId":"me"},"started":"`+started+`","timeSpentSeconds":5400}]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "W"))
	m = out.(Model)
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	_, cmd = m.handleJiraPickerKey(keyMsg(t, "y"))
	if cmd == nil {
		t.Fatal("y copied nothing")
	}
	got := fmt.Sprint(cmd())
	if !strings.Contains(got, "| 1h 30m | ABC-2 | Second \\| part |") || !strings.Contains(got, "| 1h 30m | total |") {
		t.Errorf("table = %q", got)
	}
}

func TestTimerRound(t *testing.T) {
	for _, tc := range []struct {
		elapsed, step time.Duration
		want          int
	}{
		{20 * time.Second, 0, 60},
		{7*time.Minute + 40*time.Second, 0, 8 * 60},
		{7 * time.Minute, 15 * time.Minute, 15 * 60},
		{15 * time.Minute, 15 * time.Minute, 15 * 60},
		{16 * time.Minute, 15 * time.Minute, 30 * 60},
		{0, 15 * time.Minute, 15 * 60},
	} {
		if got := timerSeconds(tc.elapsed, tc.step); got != tc.want {
			t.Errorf("%v by %v = %ds, want %ds", tc.elapsed, tc.step, got, tc.want)
		}
	}
	o, warn := optionsFrom(config.UIConfig{TimerRound: "15m"})
	if o.timerRound != 15*time.Minute || len(warn) != 0 {
		t.Errorf("timer_round = %v %v", o.timerRound, warn)
	}
	if _, warn = optionsFrom(config.UIConfig{TimerRound: "10s"}); len(warn) != 1 {
		t.Errorf("10s accepted: %v", warn)
	}
}
