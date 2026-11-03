package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/standup"
	"github.com/cornedor/laneway/internal/timesheet"
)

// Time tracking: w logs work on the panel's issue ("1h 30m what you did"),
// T starts a timer on an issue and stops it into that log, W lists what you
// logged today. The timer is kept in the store, so it outlives a restart.

// workTimer is the running timer, zero when none. With a provider that
// runs the timer (Caps.ServerTimer) it is that one: id is its running
// entry, "" until read back after a start.
type workTimer struct {
	key   string
	start time.Time
	id    string
}

// same is whether t and o are the one timer.
func (t workTimer) same(o workTimer) bool {
	return t.key == o.key && t.start.Equal(o.start) && t.id == o.id
}

// parseTimer reads the store's "KEY unix [id]".
func parseTimer(v string) workTimer {
	key, rest, _ := strings.Cut(v, " ")
	unix, id, _ := strings.Cut(rest, " ")
	sec, err := strconv.ParseInt(unix, 10, 64)
	if key == "" || err != nil {
		return workTimer{}
	}
	return workTimer{key: key, start: time.Unix(sec, 0), id: id}
}

const timerMeta = jiraMetaPrefix + "timer"

// timerTickMsg redraws the header's elapsed time.
type timerTickMsg struct{}

func timerTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return timerTickMsg{} })
}

// loadTimer reads the timer from the store: at start (a restart) and on
// each tick, so one started or stopped in laneway web shows here. A stop
// prompt being typed keeps the timer it was opened for. It reports a change.
func (m *Model) loadTimer() bool {
	if m.store == nil || m.worklogFromTimer {
		return false
	}
	v, _, _ := m.store.GetMeta(timerMeta)
	t := parseTimer(v)
	if t.same(m.timer) {
		return false
	}
	m.timer = t
	return true
}

// timerStart is Init's first tick: with a store the ticks run for good and
// load the timer, without one they run while a timer does.
func (m Model) timerStart() tea.Cmd {
	if m.store == nil {
		return nil
	}
	return func() tea.Msg { return timerTickMsg{} }
}

// saveTimer keeps the timer in the state file, for a restart.
func (m *Model) saveTimer() error {
	if m.store == nil {
		return nil
	}
	v := ""
	if m.timer.key != "" {
		v = m.timer.key + " " + strconv.FormatInt(m.timer.start.Unix(), 10)
		if m.timer.id != "" {
			v += " " + m.timer.id
		}
	}
	return m.store.SetMeta(timerMeta, v)
}

func (m Model) handleTimerTick() (tea.Model, tea.Cmd) {
	if m.loadTimer() || m.timer.key != "" {
		m.redrawTimed()
	}
	if m.store == nil && m.timer.key == "" && m.sheetProvider == nil {
		return m, nil
	}
	return m, tea.Batch(timerTick(), m.syncTimer())
}

// serverTimer is whether the timesheet runs the timer itself.
func (m *Model) serverTimer() bool { return m.sheet().Caps().ServerTimer }

// timerSyncMsg is the backend's running entry as read; err is a failed
// timer action (op) or read.
type timerSyncMsg struct {
	run    *timesheet.Entry
	server bool
	op     string
	err    error
}

// syncTimer reads the backend's running entry, so a timer started or
// stopped elsewhere shows here. Not while its stop prompt is open.
func (m *Model) syncTimer() tea.Cmd {
	if m.sheetProvider == nil || m.worklogFromTimer {
		return nil
	}
	sh, ctx := m.sheetProvider, m.ctx
	return func() tea.Msg {
		run, err := sh.Running(ctx)
		return timerSyncMsg{run: run, server: sh.Caps().ServerTimer, err: err}
	}
}

// timerOp runs op on the backend's running entry (its id read first when
// the timer has none yet), then reads the timer back.
func (m *Model) timerOp(what string, op func(sh timesheet.Provider, id string) error) tea.Cmd {
	sh, ctx, t := m.sheet(), m.ctx, m.timer
	return func() tea.Msg {
		id, err := runningID(ctx, sh, t)
		if err == nil {
			err = op(sh, id)
		}
		run, rerr := sh.Running(ctx)
		if err == nil {
			err = rerr
		}
		return timerSyncMsg{run: run, server: true, op: what, err: err}
	}
}

// runningID is t's entry on the backend: its id, else the running one's
// when it is on t's issue.
func runningID(ctx context.Context, sh timesheet.Provider, t workTimer) (string, error) {
	if t.id != "" {
		return t.id, nil
	}
	run, err := sh.Running(ctx)
	switch {
	case err != nil:
		return "", err
	case run == nil || run.Key != t.key:
		return "", errors.New(i18n.Tf("the timer on %s no longer runs in %s", t.key, sh.Name()))
	}
	return run.ID, nil
}

// handleTimerSync takes the backend's timer as laneway's. A failed read
// goes to the log once per error; a failed action shows.
func (m Model) handleTimerSync(msg timerSyncMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if text := i18n.Tf("timer: %s", msg.err.Error()); msg.op != "" {
			m.fail(text)
		} else if text != m.timerSyncErr {
			m.logError(text)
		}
		m.timerSyncErr = msg.err.Error()
		if msg.op == "" {
			return m, nil
		}
		return m, m.syncTimer()
	}
	m.timerSyncErr = ""
	if !msg.server || m.worklogFromTimer {
		return m, nil
	}
	var t workTimer
	if r := msg.run; r != nil && r.Key != "" { // an entry without an issue is no timer of laneway's
		t = workTimer{key: r.Key, start: time.Unix(r.Started.Unix(), 0), id: r.ID}
	}
	if t.same(m.timer) {
		return m, nil
	}
	m.timer = t
	m.redrawTimed()
	if err := m.saveTimer(); err != nil {
		m.logError(i18n.Tf("timer: %s", err.Error()))
	}
	return m, nil
}

// timerMark is "⏱ 12m" on the timed issue's card, row and panel; "" on
// any other.
func (m *Model) timerMark(key string) string {
	if m.timer.key == "" || m.timer.key != key {
		return ""
	}
	return jiraPinStyle.Render("⏱ " + jira.FormatDuration(max(int(time.Since(m.timer.start).Seconds()), 0)))
}

// redrawTimed redraws the board and panel with the timer's mark as it is.
func (m *Model) redrawTimed() {
	if m.jiraTab != nil {
		m.jiraTab.rows = nil
		m.renderJira()
	}
	if m.refOpen {
		m.renderRef()
	}
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
		if key != t.key {
			m.timerNext = key
		}
		return nil
	}
	if key == "" {
		return nil
	}
	m.timer = workTimer{key: key, start: time.Unix(time.Now().Unix(), 0)} // as the store keeps it
	m.redrawTimed()
	m.status = i18n.Tf("timer started on %s · %s stops it", key, helpKey(m.keys.Timer))
	if err := m.saveTimer(); err != nil {
		m.fail(i18n.Tf("timer started on %s, but a restart loses it: %s", key, err.Error()))
	}
	var start tea.Cmd
	if m.serverTimer() {
		sh, ctx, at := m.sheet(), m.ctx, m.timer.start
		start = func() tea.Msg {
			err := sh.Start(ctx, key, "", at)
			run, rerr := sh.Running(ctx)
			if err == nil {
				err = rerr
			}
			return timerSyncMsg{run: run, server: true, op: "start", err: err}
		}
	}
	if m.store != nil {
		return start // the ticks run already
	}
	return tea.Batch(start, timerTick())
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
	ti.Placeholder = i18n.T("1h 30m what you did (yesterday 2h: another day; left:2h: what's left)")
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
	m.worklogFromTimer, m.timerNext, m.timerDropArmed = false, "", false
}

// timerDropAfter is how long a timer runs before dropping it unlogged asks
// twice.
const timerDropAfter = 5 * time.Minute

// timerStopKey handles the timer's stop prompt's own keys: ctrl+d drops
// the timer unlogged (twice once it ran a while), ctrl+t moves it to the
// card T was pressed on. ok is false for any other key.
func (m Model) timerStopKey(msg tea.KeyPressMsg) (_ tea.Model, _ tea.Cmd, ok bool) {
	ran := jira.FormatDuration(max(int(time.Since(m.timer.start).Seconds()), 0))
	var cmd tea.Cmd
	switch msg.String() {
	case "ctrl+d":
		if !m.timerDropArmed && time.Since(m.timer.start) >= timerDropAfter {
			m.timerDropArmed = true
			m.status = i18n.Tf("ctrl+d again drops the timer: %s on %s not logged", ran, m.timer.key)
			return m, nil, true
		}
		key := m.timer.key
		if m.serverTimer() {
			cmd = m.timerOp("drop", func(sh timesheet.Provider, id string) error {
				return sh.Delete(m.ctx, key, []string{id})
			})
		}
		m.timer = workTimer{}
		m.status = i18n.Tf("timer dropped: %s on %s not logged", ran, key)
	case "ctrl+t":
		if m.timerNext == "" {
			return m, nil, false
		}
		if m.serverTimer() {
			if sh := m.sheet(); !sh.Caps().Move {
				m.fail(i18n.Tf("%s can't move worklogs", sh.Name()))
				return m, nil, true
			}
			from, to := m.timer.key, m.timerNext
			cmd = m.timerOp("move", func(sh timesheet.Provider, id string) error {
				return sh.Move(m.ctx, from, []string{id}, to)
			})
		}
		m.timer.key = m.timerNext
		m.status = i18n.Tf("timer moved to %s, %s on it", m.timerNext, ran)
	default:
		m.timerDropArmed = false
		return m, nil, false
	}
	if err := m.saveTimer(); err != nil {
		m.fail(i18n.Tf("timer: %s", err.Error()))
	}
	m.closeJiraField()
	m.redrawTimed()
	return m, cmd, true
}

// worklogLeft takes a left:2h (the remaining estimate after this work) or
// left:keep word out of a worklog input.
func worklogLeft(raw string) (left, rest string, err error) {
	words := strings.Fields(raw)
	i := slices.IndexFunc(words, func(w string) bool { return strings.HasPrefix(strings.ToLower(w), "left:") })
	if i < 0 {
		return "", raw, nil
	}
	left = words[i][len("left:"):]
	rest = strings.Join(slices.Delete(words, i, i+1), " ")
	if strings.EqualFold(left, jira.LeftKeep) {
		return jira.LeftKeep, rest, nil
	}
	if secs, extra, err := jira.ParseDuration(left); err != nil || secs == 0 || extra != "" {
		return "", "", errors.New(i18n.Tf("left:%s is not a time (left:2h, left:keep)", left))
	}
	return left, rest, nil
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
	left, raw, err := worklogLeft(raw)
	switch {
	case err != nil || left == "":
	case m.worklogEdit != "":
		err = errors.New(i18n.T("left: is for new work, not an edit"))
	case !m.sheet().Caps().Remaining:
		err = errors.New(i18n.Tf("left: sets Jira's estimate; %s has none", m.sheet().Name()))
	}
	if err != nil {
		m.fail(err.Error())
		return m, nil
	}
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
		sh, ctx := m.sheet(), m.ctx
		reload := m.openTimesheetDay(showDay)
		m.status = i18n.Tf("updating the worklog on %s…", key)
		return m, func() tea.Msg {
			if err := sh.Update(ctx, key, id, secs, moved, newComment); err != nil {
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
	sh, ctx, fromTimer, next := m.sheet(), m.ctx, m.worklogFromTimer, m.timerNext
	m.status = i18n.Tf("logging %s on %s…", jira.FormatDuration(secs), key)
	if fromTimer && m.serverTimer() {
		// The backend's running entry stops with the time and comment typed.
		t, moved := m.timer, time.Time{}
		if onDay {
			moved = started
		}
		return m, func() tea.Msg {
			id, err := runningID(ctx, sh, t)
			if err == nil {
				err = sh.Update(ctx, key, id, secs, moved, &comment)
			}
			return worklogLoggedMsg{key: key, fromTimer: true, next: next, err: err}
		}
	}
	return m, func() tea.Msg {
		err := sh.Add(ctx, timesheet.New{Key: key, Seconds: secs, Started: started, Comment: comment, Left: left})
		return worklogLoggedMsg{key: key, fromTimer: fromTimer, next: next, err: err}
	}
}

// worklogLoggedMsg is a worklog written; one from the timer stops it only
// now, so a failed write keeps its time.
type worklogLoggedMsg struct {
	key       string
	fromTimer bool
	next      string // the card the timer goes on next, "" for none
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
	stopped := msg.fromTimer && msg.err == nil && m.timer.key == msg.key
	if stopped {
		m.timer = workTimer{}
		m.redrawTimed()
		if err := m.saveTimer(); err != nil {
			m.logError(i18n.Tf("timer: still in the state file, a restart brings it back: %s", err.Error()))
		}
	}
	if msg.err != nil && msg.fromTimer {
		msg.err = fmt.Errorf("%w (%s)", msg.err, i18n.T("the timer keeps running"))
	}
	out, cmd := m.handleJiraMutated(jiraMutatedMsg{key: msg.key, field: "worklog", err: msg.err})
	if m = out.(Model); m.jiraTab.week != nil && msg.err == nil {
		cmd = tea.Batch(cmd, m.loadWeek())
	}
	if stopped && msg.next != "" {
		logged := m.status
		cmd = tea.Batch(cmd, m.toggleTimer(msg.next))
		m.status = i18n.Tf("%s · timer started on %s", logged, msg.next)
	}
	return m, cmd
}

// openTimesheet lists today's worklogs of yours; enter opens the issue.
func (m *Model) openTimesheet() tea.Cmd {
	return m.openTimesheetDay(time.Now())
}

// openTimesheetDay lists day's worklogs of yours. [ ] step a day, x marks
// entries, d twice deletes the marked ones (else the one under the cursor),
// m moves them to another issue, y copies the day as a markdown table.
func (m *Model) openTimesheetDay(day time.Time) tea.Cmd {
	gen := m.startJiraPicker(jiraPickTimesheet, standup.Day(day, time.Now()), false)
	m.jiraPicker.day = day
	seq := m.jiraPicker.fetchSeq
	sh, ctx, k := m.sheet(), m.ctx, m.keys
	empty := i18n.Tf("nothing logged · %s in the panel logs work · %s %s another day", helpKey(k.LogWork), helpKey(k.PrevView), helpKey(k.NextView))
	hint := i18n.Tf("  ·  %s %s day · %s edit · %s %s delete · %s mark", helpKey(k.PrevView), helpKey(k.NextView),
		helpKey(k.EditEntry), helpKey(k.DeleteEntry), helpKey(k.DeleteEntry), helpKey(k.Mark))
	if sh.Caps().Move {
		hint += i18n.Tf(" · %s move", helpKey(k.MoveEntry))
	}
	hint += i18n.Tf(" · %s propose · %s copy · %s week", helpKey(k.ProposeWork), helpKey(k.CopyKey), helpKey(k.Timesheet))
	return func() tea.Msg {
		logs, err := timesheet.Day(ctx, sh, day)
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
		rows = append(rows, []string{"", jira.FormatDuration(total), i18n.T("total"), "", ""})
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickTimesheet, items: items, err: err,
			title: standup.Day(day, time.Now()) + " — " + jira.FormatDuration(total) + hint,
			text:  markdownTable([]string{i18n.T("Started"), i18n.T("Time"), i18n.T("Issue"), i18n.T("Summary"), i18n.T("Comment")}, rows)}
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
			m.status = i18n.T("today is the last day to show")
			return nil, true
		}
		return m.openTimesheetDay(p.day.AddDate(0, 0, 1)), true
	case is(m.keys.ProposeWork):
		if p.loading || p.err != nil {
			return nil, true
		}
		return m.loadProposals(), true
	case is(m.keys.CopyKey):
		if p.loading || p.err != nil {
			return nil, true
		}
		m.status = i18n.T("copied the day as a markdown table")
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
	case is(m.keys.Mark):
		if p.idx >= len(p.items) || !timesheetRow(p.items[p.idx]) {
			return nil, true
		}
		p.items[p.idx].current = !p.items[p.idx].current
		p.idx = min(p.idx+1, len(p.items)-1)
		m.status = m.timesheetMarked()
		return nil, true
	case is(m.keys.MarkAll):
		all := !slices.ContainsFunc(p.items, func(it jiraPickerItem) bool { return timesheetRow(it) && !it.current })
		for i := range p.items {
			if timesheetRow(p.items[i]) {
				p.items[i].current = !all
			}
		}
		m.status = m.timesheetMarked()
		return nil, true
	case is(m.keys.DeleteEntry):
		rows := p.timesheetTargets()
		if len(rows) == 0 {
			return nil, true
		}
		if sig := strings.Join(rows, ","); p.pendingDelete != sig {
			p.pendingDelete = sig
			m.status = i18n.Tf("%s again deletes this worklog", helpKey(m.keys.DeleteEntry))
			if len(rows) > 1 {
				m.status = i18n.Tf("%s again deletes %d worklogs", helpKey(m.keys.DeleteEntry), len(rows))
			}
			return nil, true
		}
		keys, ids := worklogsByIssue(rows)
		day, sh, ctx := p.day, m.sheet(), m.ctx
		m.status = i18n.Tf("deleting a worklog on %s…", keys[0])
		if len(rows) > 1 {
			m.status = i18n.Tf("deleting %d worklogs…", len(rows))
		}
		reload := m.openTimesheetDay(day)
		return func() tea.Msg {
			for _, key := range keys { // the API takes one issue's at a time
				if err := sh.Delete(ctx, key, ids[key]); err != nil {
					return worklogFailedMsg{key: key, day: day, err: err}
				}
			}
			return reload()
		}, true
	case is(m.keys.MoveEntry):
		rows := p.timesheetTargets()
		if len(rows) == 0 {
			return nil, true
		}
		if sh := m.sheet(); !sh.Caps().Move {
			m.status = i18n.T("this Jira can't move worklogs")
			if sh.Name() != "Jira" {
				m.status = i18n.Tf("%s can't move worklogs", sh.Name())
			}
			return nil, true
		}
		day := p.day
		m.closeJiraPicker()
		m.openBulkInput("worklog-move", i18n.T("ABC-123 or 123"))
		m.jiraFieldKey = i18n.Tn(len(rows), "%d worklog", "%d worklogs", len(rows))
		m.worklogMoves, m.worklogEditDay = rows, day
		return nil, true
	}
	return nil, false
}

// timesheetRow is whether it is one of the day's worklogs (key/id), not a
// proposal or the empty day's line.
func timesheetRow(it jiraPickerItem) bool {
	return strings.Contains(it.id, "/") && !strings.HasPrefix(it.id, proposalID)
}

// timesheetTargets are the marked worklogs (key/id), else the one under
// the cursor.
func (p *jiraPickerState) timesheetTargets() []string {
	var rows []string
	for _, it := range p.items {
		if it.current && timesheetRow(it) {
			rows = append(rows, it.id)
		}
	}
	if len(rows) == 0 && p.idx < len(p.items) && timesheetRow(p.items[p.idx]) {
		rows = []string{p.items[p.idx].id}
	}
	return rows
}

// timesheetMarked says how many entries are marked and what takes them.
func (m *Model) timesheetMarked() string {
	n := 0
	for _, it := range m.jiraPicker.items {
		if it.current && timesheetRow(it) {
			n++
		}
	}
	if n == 0 {
		return i18n.T("no worklogs marked")
	}
	return i18n.Tf("%d marked · %s %s deletes them · %s moves them", n, helpKey(m.keys.DeleteEntry), helpKey(m.keys.DeleteEntry), helpKey(m.keys.MoveEntry))
}

// worklogsByIssue splits key/id rows by issue, the keys in the order seen.
func worklogsByIssue(rows []string) (keys []string, ids map[string][]string) {
	ids = map[string][]string{}
	for _, r := range rows {
		key, id, _ := strings.Cut(r, "/")
		if _, ok := ids[key]; !ok {
			keys = append(keys, key)
		}
		ids[key] = append(ids[key], id)
	}
	return keys, ids
}

// applyWorklogMove moves the input's worklogs to the typed issue, one
// request per issue they're on, then shows their day again.
func (m Model) applyWorklogMove(raw string) (tea.Model, tea.Cmd) {
	to := jiraGotoKey(raw, m.jiraTab.project)
	if to == "" {
		m.fail(i18n.Tf("not an issue key: %s", raw))
		return m, nil
	}
	rows, day := m.worklogMoves, m.worklogEditDay
	m.worklogMoves = nil
	m.closeJiraField()
	keys, ids := worklogsByIssue(rows)
	keys = slices.DeleteFunc(keys, func(k string) bool { return k == to }) // there already
	sh, ctx := m.sheet(), m.ctx
	reload := m.openTimesheetDay(day)
	m.status = i18n.Tf("moving %s to %s…", i18n.Tn(len(rows), "%d worklog", "%d worklogs", len(rows)), to)
	return m, func() tea.Msg {
		for _, key := range keys {
			if err := sh.Move(ctx, key, ids[key], to); err != nil {
				return worklogFailedMsg{key: key, day: day, err: err}
			}
		}
		return reload()
	}
}
