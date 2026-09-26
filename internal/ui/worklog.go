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
		// The timer keeps running until the log is in: esc or a failed
		// write leaves it, and its time, as it was.
		secs := max(int(time.Since(t.start).Round(time.Minute).Seconds()), 60)
		m.openWorklogInput(t.key, jira.FormatDuration(secs)+" ", t.start)
		m.worklogFromTimer = true
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
	m.worklogEdit = "" // a new entry, unless the caller says otherwise
	m.worklogFromTimer = false
}

// applyWorklog logs the input's time and comment.
func (m Model) applyWorklog(raw string) (tea.Model, tea.Cmd) {
	secs, comment, err := jira.ParseDuration(raw)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	key, started := m.jiraFieldKey, m.worklogStart
	if id := m.worklogEdit; id != "" {
		day := m.worklogEditDay
		m.worklogEdit = ""
		m.closeJiraField()
		c, ctx := m.jiraClient, m.ctx
		reload := m.openTimesheetDay(day)
		m.status = "updating the worklog on " + key + "…"
		return m, func() tea.Msg {
			if err := c.UpdateWorklog(ctx, key, id, secs, comment); err != nil {
				return jiraMutatedMsg{key: key, field: "worklog", err: err}
			}
			return reload()
		}
	}
	if started.IsZero() {
		started = time.Now().Add(-time.Duration(secs) * time.Second)
	}
	m.closeJiraField()
	c, ctx, fromTimer := m.jiraClient, m.ctx, m.worklogFromTimer
	m.status = fmt.Sprintf("logging %s on %s…", jira.FormatDuration(secs), key)
	return m, func() tea.Msg {
		return worklogLoggedMsg{key: key, fromTimer: fromTimer, err: c.AddWorklog(ctx, key, secs, started, comment)}
	}
}

// worklogLoggedMsg is a worklog written; one from the timer stops it only
// now, so a failed write keeps its time.
type worklogLoggedMsg struct {
	key       string
	fromTimer bool
	err       error
}

func (m Model) handleWorklogLogged(msg worklogLoggedMsg) (tea.Model, tea.Cmd) {
	if msg.fromTimer && msg.err == nil && m.timer.key == msg.key {
		m.timer = workTimer{}
		m.saveTimer()
	}
	if msg.err != nil && msg.fromTimer {
		msg.err = fmt.Errorf("%w (the timer keeps running)", msg.err)
	}
	return m.handleJiraMutated(jiraMutatedMsg{key: msg.key, field: "worklog", err: msg.err})
}

// openTimesheet lists today's worklogs of yours; enter opens the issue.
func (m *Model) openTimesheet() tea.Cmd {
	return m.openTimesheetDay(time.Now())
}

// openTimesheetDay lists day's worklogs of yours. [ ] step a day, d twice
// deletes the entry under the cursor.
func (m *Model) openTimesheetDay(day time.Time) tea.Cmd {
	gen := m.startJiraPicker(jiraPickTimesheet, standupDay(day, time.Now()), false)
	m.jiraPicker.day = day
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		logs, err := c.MyWorklogs(ctx, day)
		total := 0
		items := make([]jiraPickerItem, len(logs))
		for i, w := range logs {
			total += w.Seconds
			label := fmt.Sprintf("%s  %6s  %s %s", w.Started.Local().Format("15:04"), jira.FormatDuration(w.Seconds), w.Key, w.Summary)
			if w.Comment != "" {
				label += " — " + strings.ReplaceAll(w.Comment, "\n", " ")
			}
			items[i] = jiraPickerItem{id: w.Key + "/" + w.ID, label: label, value: jira.FormatDuration(w.Seconds) + " " + w.Comment}
		}
		if err == nil && len(items) == 0 {
			items = []jiraPickerItem{{label: "nothing logged"}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickTimesheet, items: items, err: err,
			title: standupDay(day, time.Now()) + " — " + jira.FormatDuration(total) + "  ·  [ ] day · e edit · d d delete"}
	}
}

// timesheetKey handles the timesheet's own keys; false when k is not one.
func (m *Model) timesheetKey(k string) (tea.Cmd, bool) {
	p := &m.jiraPicker
	if k != "d" && k != "delete" {
		p.pendingDelete = "" // a delete is confirmed by the very next key only
	}
	switch {
	case k == helpKey(m.keys.PrevView):
		return m.openTimesheetDay(p.day.AddDate(0, 0, -1)), true
	case k == helpKey(m.keys.NextView):
		return m.openTimesheetDay(p.day.AddDate(0, 0, 1)), true
	case k == "e":
		if p.idx >= len(p.items) || !strings.Contains(p.items[p.idx].id, "/") {
			return nil, true
		}
		it := p.items[p.idx]
		key, id, _ := strings.Cut(it.id, "/")
		day := p.day
		m.closeJiraPicker()
		m.openWorklogInput(key, strings.TrimSpace(it.value), time.Time{})
		m.worklogEdit, m.worklogEditDay = id, day
		return nil, true
	case k == "d" || k == "delete":
		if p.idx >= len(p.items) || !strings.Contains(p.items[p.idx].id, "/") {
			return nil, true
		}
		it := p.items[p.idx]
		if p.pendingDelete != it.id {
			p.pendingDelete = it.id
			m.status = "d again deletes this worklog"
			return nil, true
		}
		key, id, _ := strings.Cut(it.id, "/")
		day, c, ctx := p.day, m.jiraClient, m.ctx
		m.status = "deleting a worklog on " + key + "…"
		reload := m.openTimesheetDay(day)
		return func() tea.Msg {
			if err := c.DeleteWorklog(ctx, key, id); err != nil {
				return jiraMutatedMsg{key: key, field: "worklog", err: err}
			}
			return reload()
		}, true
	}
	return nil, false
}
