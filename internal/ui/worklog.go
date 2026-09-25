package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// Time tracking: w logs work on the panel's issue ("1h 30m what you did"),
// T starts a timer on an issue and stops it into that log, W lists what you
// logged today. The timer is kept in the store, so it outlives a restart.

// workTimer is the running timer, zero when none.
type workTimer struct {
	key   string
	start time.Time
}

const timerMeta = jiraMetaPrefix + "timer"

// timerTickMsg redraws the header's elapsed time.
type timerTickMsg struct{}

func timerTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return timerTickMsg{} })
}

// loadTimer restores a timer left running, with its tick.
func (m *Model) loadTimer() tea.Cmd {
	if m.store == nil {
		return nil
	}
	v, ok, _ := m.store.GetMeta(timerMeta)
	key, unix, _ := strings.Cut(v, " ")
	sec, err := strconv.ParseInt(unix, 10, 64)
	if !ok || key == "" || err != nil {
		return nil
	}
	m.timer = workTimer{key: key, start: time.Unix(sec, 0)}
	return timerTick()
}

func (m *Model) saveTimer() {
	if m.store == nil {
		return
	}
	v := ""
	if m.timer.key != "" {
		v = m.timer.key + " " + strconv.FormatInt(m.timer.start.Unix(), 10)
	}
	_ = m.store.SetMeta(timerMeta, v)
}

func (m Model) handleTimerTick() (tea.Model, tea.Cmd) {
	if m.timer.key == "" {
		return m, nil
	}
	return m, timerTick()
}

// toggleTimer stops a running timer into its log input, or starts one on
// key.
func (m *Model) toggleTimer(key string) tea.Cmd {
	if t := m.timer; t.key != "" {
		m.timer = workTimer{}
		m.saveTimer()
		secs := max(int(time.Since(t.start).Round(time.Minute).Seconds()), 60)
		m.openWorklogInput(t.key, jira.FormatDuration(secs)+" ", t.start)
		return nil
	}
	if key == "" {
		return nil
	}
	m.timer = workTimer{key: key, start: time.Now()}
	m.saveTimer()
	m.status = "timer started on " + key + " · " + helpKey(m.keys.Timer) + " stops it"
	return timerTick()
}

// timerLabel is the header's "⏱ ABC-1 12m", "" without a timer.
func (m *Model) timerLabel() string {
	if m.timer.key == "" {
		return ""
	}
	return "⏱ " + m.timer.key + " " + jira.FormatDuration(max(int(time.Since(m.timer.start).Seconds()), 0))
}

// openWorklogInput asks the time and comment to log on key; started is when
// the work began, zero for "the time logged, back from now".
func (m *Model) openWorklogInput(key, value string, started time.Time) {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "1h 30m what you did"
	ti.SetWidth(max(min(m.width-16, 60), 16))
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Focus()
	m.jiraFieldInput = ti
	m.jiraFieldActive = true
	m.jiraFieldName = "worklog"
	m.jiraFieldKey = key
	m.worklogStart = started
}

// applyWorklog logs the input's time and comment.
func (m Model) applyWorklog(raw string) (tea.Model, tea.Cmd) {
	secs, comment, err := jira.ParseDuration(raw)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	key, started := m.jiraFieldKey, m.worklogStart
	if started.IsZero() {
		started = time.Now().Add(-time.Duration(secs) * time.Second)
	}
	m.closeJiraField()
	c, ctx := m.jiraClient, m.ctx
	m.status = fmt.Sprintf("logging %s on %s…", jira.FormatDuration(secs), key)
	return m, jiraMutateCmd(key, "worklog", func() error { return c.AddWorklog(ctx, key, secs, started, comment) })
}

// openTimesheet lists today's worklogs of yours; enter opens the issue.
func (m *Model) openTimesheet() tea.Cmd {
	gen := m.startJiraPicker(jiraPickTimesheet, "Today", false)
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		logs, err := c.MyWorklogs(ctx, time.Now())
		total := 0
		items := make([]jiraPickerItem, len(logs))
		for i, w := range logs {
			total += w.Seconds
			label := fmt.Sprintf("%s  %6s  %s %s", w.Started.Local().Format("15:04"), jira.FormatDuration(w.Seconds), w.Key, w.Summary)
			if w.Comment != "" {
				label += " — " + strings.ReplaceAll(w.Comment, "\n", " ")
			}
			items[i] = jiraPickerItem{id: w.Key, label: label}
		}
		if err == nil && len(items) == 0 {
			items = []jiraPickerItem{{label: "nothing logged yet today"}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickTimesheet, items: items, err: err,
			title: "Today — " + jira.FormatDuration(total)}
	}
}
