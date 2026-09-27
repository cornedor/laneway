package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
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

// saveTimer keeps the timer in the state file, for a restart.
func (m *Model) saveTimer() error {
	if m.store == nil {
		return nil
	}
	v := ""
	if m.timer.key != "" {
		v = m.timer.key + " " + strconv.FormatInt(m.timer.start.Unix(), 10)
	}
	return m.store.SetMeta(timerMeta, v)
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
		secs := timerSeconds(time.Since(t.start), m.opts.timerRound)
		m.openWorklogInput(t.key, jira.FormatDuration(secs)+" ", t.start)
		m.worklogFromTimer = true
		return nil
	}
	if key == "" {
		return nil
	}
	m.timer = workTimer{key: key, start: time.Now()}
	m.status = "timer started on " + key + " · " + helpKey(m.keys.Timer) + " stops it"
	if err := m.saveTimer(); err != nil {
		m.fail("timer started on " + key + ", but a restart loses it: " + err.Error())
	}
	return timerTick()
}

// timerSeconds is the time a stopped timer logs: to the minute (at least
// one), or rounded up to ui.timer_round's step (at least one step).
func timerSeconds(elapsed, step time.Duration) int {
	if step <= 0 {
		return max(int(elapsed.Round(time.Minute).Seconds()), 60)
	}
	steps := max((elapsed+step-1)/step, 1)
	return int((steps * step).Seconds())
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
	ti.Placeholder = "1h 30m what you did (yesterday 2h: another day)"
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

// worklogDay reads a leading day off a worklog input: "yesterday 2h",
// "fri 1h" (the last Friday, today on a Friday), "2026-09-21 3h", "-2d 1h".
// ok is false when the input starts with the time.
func worklogDay(raw string, now time.Time) (day time.Time, rest string, ok bool) {
	word, rest, _ := strings.Cut(strings.TrimSpace(raw), " ")
	w := strings.ToLower(word)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if len(w) >= 3 && strings.HasPrefix(strings.ToLower(wd.String()), w) {
			return today.AddDate(0, 0, -((int(today.Weekday()) - int(wd) + 7) % 7)), rest, true
		}
	}
	if w == "today" || w == "yesterday" || strings.HasPrefix(w, "-") || strings.Count(w, "-") == 2 {
		if d, err := jira.ParseDate(w, now); err == nil {
			return d, rest, true
		}
	}
	return time.Time{}, raw, false
}

// applyWorklog logs the input's time and comment; a leading day
// ("yesterday 2h") logs it then, from ui.workday_start; in an edit it moves
// the entry there.
func (m Model) applyWorklog(raw string) (tea.Model, tea.Cmd) {
	day, raw, onDay := worklogDay(raw, time.Now())
	secs, comment, err := jira.ParseDuration(raw)
	if err != nil {
		m.fail(err.Error())
		return m, nil
	}
	key, started := m.jiraFieldKey, m.worklogStart
	if id := m.worklogEdit; id != "" {
		showDay, moved := m.worklogEditDay, time.Time{}
		if onDay { // a day first moves the entry there
			showDay, moved = day, day.Add(m.opts.workdayStart)
		}
		var newComment *string
		if comment != m.worklogComment {
			newComment = &comment
		}
		input, editDay := m.jiraFieldInput.Value(), m.worklogEditDay
		m.worklogEdit = ""
		m.closeJiraField()
		c, ctx := m.jiraClient, m.ctx
		reload := m.openTimesheetDay(showDay)
		m.status = "updating the worklog on " + key + "…"
		return m, func() tea.Msg {
			if err := c.UpdateWorklog(ctx, key, id, secs, moved, newComment); err != nil {
				return worklogFailedMsg{key: key, id: id, day: editDay, input: input, err: err}
			}
			return reload()
		}
	}
	switch {
	case onDay: // a typed day beats the timer's start
		started = day.Add(m.opts.workdayStart)
	case started.IsZero():
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

// worklogFailedMsg is a timesheet edit (input the typed text) or delete
// that failed.
type worklogFailedMsg struct {
	key, id string
	day     time.Time
	input   string
	err     error
}

// handleWorklogFailed says why, then brings back the edit with its text,
// or the day's timesheet after a delete, unless the timesheet was closed.
func (m Model) handleWorklogFailed(msg worklogFailedMsg) (tea.Model, tea.Cmd) {
	out, _ := m.handleJiraMutated(jiraMutatedMsg{key: msg.key, field: "worklog", err: msg.err})
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickTimesheet {
		return m, nil
	}
	m.closeJiraPicker()
	if msg.input == "" {
		return m, m.openTimesheetDay(msg.day)
	}
	comment := m.worklogComment
	m.openWorklogInput(msg.key, msg.input, time.Time{})
	m.worklogEdit, m.worklogEditDay, m.worklogComment = msg.id, msg.day, comment
	return m, nil
}

func (m Model) handleWorklogLogged(msg worklogLoggedMsg) (tea.Model, tea.Cmd) {
	if msg.fromTimer && msg.err == nil && m.timer.key == msg.key {
		m.timer = workTimer{}
		if err := m.saveTimer(); err != nil {
			m.logError("timer: still in the state file, a restart brings it back: " + err.Error())
		}
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
// deletes the entry under the cursor, y copies the day as a markdown table.
func (m *Model) openTimesheetDay(day time.Time) tea.Cmd {
	gen := m.startJiraPicker(jiraPickTimesheet, standupDay(day, time.Now()), false)
	m.jiraPicker.day = day
	seq := m.jiraPicker.fetchSeq
	c, ctx, k := m.jiraClient, m.ctx, m.keys
	empty := fmt.Sprintf("nothing logged · %s in the panel logs work · %s %s another day", helpKey(k.LogWork), helpKey(k.PrevView), helpKey(k.NextView))
	hint := fmt.Sprintf("  ·  %s %s day · %s edit · %s %s delete · %s copy", helpKey(k.PrevView), helpKey(k.NextView),
		helpKey(k.EditEntry), helpKey(k.DeleteEntry), helpKey(k.DeleteEntry), helpKey(k.CopyKey))
	return func() tea.Msg {
		logs, err := c.MyWorklogs(ctx, day)
		total := 0
		items := make([]jiraPickerItem, len(logs))
		rows := make([][]string, len(logs))
		for i, w := range logs {
			total += w.Seconds
			rows[i] = []string{w.Started.Local().Format("15:04"), jira.FormatDuration(w.Seconds), w.Key, w.Summary, w.Comment}
			label := fmt.Sprintf("%s  %6s  %s %s", w.Started.Local().Format("15:04"), jira.FormatDuration(w.Seconds), w.Key, w.Summary)
			if w.Comment != "" {
				label += " — " + strings.ReplaceAll(w.Comment, "\n", " ")
			}
			items[i] = jiraPickerItem{id: w.Key + "/" + w.ID, label: label, value: jira.FormatDuration(w.Seconds) + " " + w.Comment}
		}
		if err == nil && len(items) == 0 {
			items = []jiraPickerItem{{label: empty}}
		}
		rows = append(rows, []string{"", jira.FormatDuration(total), "total", "", ""})
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickTimesheet, items: items, err: err,
			title: standupDay(day, time.Now()) + " — " + jira.FormatDuration(total) + hint,
			text:  markdownTable([]string{"Started", "Time", "Issue", "Summary", "Comment"}, rows)}
	}
}

// timesheetKey handles the timesheet's own keys; false when k is not one.
func (m *Model) timesheetKey(k string) (tea.Cmd, bool) {
	p := &m.jiraPicker
	is := func(b key.Binding) bool { return slices.Contains(b.Keys(), k) }
	if !is(m.keys.DeleteEntry) {
		p.pendingDelete = "" // a delete is confirmed by the very next key only
	}
	switch {
	case k == helpKey(m.keys.PrevView):
		return m.openTimesheetDay(p.day.AddDate(0, 0, -1)), true
	case k == helpKey(m.keys.NextView):
		if y, mo, d := time.Now().Date(); !p.day.Before(time.Date(y, mo, d, 0, 0, 0, 0, time.Local)) {
			m.status = "today is the last day to show"
			return nil, true
		}
		return m.openTimesheetDay(p.day.AddDate(0, 0, 1)), true
	case is(m.keys.CopyKey):
		if p.loading || p.err != nil {
			return nil, true
		}
		m.status = "copied the day as a markdown table"
		return tea.SetClipboard(p.text), true
	case is(m.keys.EditEntry):
		if p.idx >= len(p.items) || !strings.Contains(p.items[p.idx].id, "/") {
			return nil, true
		}
		it := p.items[p.idx]
		key, id, _ := strings.Cut(it.id, "/")
		day := p.day
		m.closeJiraPicker()
		m.openWorklogInput(key, strings.TrimSpace(it.value), time.Time{})
		_, comment, _ := jira.ParseDuration(m.jiraFieldInput.Value())
		m.worklogEdit, m.worklogEditDay, m.worklogComment = id, day, comment
		return nil, true
	case is(m.keys.DeleteEntry):
		if p.idx >= len(p.items) || !strings.Contains(p.items[p.idx].id, "/") {
			return nil, true
		}
		it := p.items[p.idx]
		if p.pendingDelete != it.id {
			p.pendingDelete = it.id
			m.status = helpKey(m.keys.DeleteEntry) + " again deletes this worklog"
			return nil, true
		}
		key, id, _ := strings.Cut(it.id, "/")
		day, c, ctx := p.day, m.jiraClient, m.ctx
		m.status = "deleting a worklog on " + key + "…"
		reload := m.openTimesheetDay(day)
		return func() tea.Msg {
			if err := c.DeleteWorklog(ctx, key, id); err != nil {
				return worklogFailedMsg{key: key, day: day, err: err}
			}
			return reload()
		}, true
	}
	return nil, false
}
