package ui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// I swaps the board for the inbox, as U does the standup: what others did
// on your issues in the last ui.inbox_lookback, one thread per issue, on
// every configured site. Threads are listed on the left, the cursor's news
// in full on the right. Each thread keeps its own marks: read once shown,
// done (e) until something new happens on it, snoozed (s) until the next
// workday. The header's ✉ counts the unread threads.
//
// The threads are kept between opens and synced every ui.inbox_every: one
// search per site, then only the issues updated since are read again.

const (
	inboxMarksMeta = jiraMetaPrefix + "inbox_marks"
	// inboxFloorMeta is when the inbox was first read (or, from before
	// threads had marks, last read): news before it counts as read.
	inboxFloorMeta = jiraMetaPrefix + "inbox_seen"
	// inboxUnreadMeta is the header's count, for laneway prompt.
	inboxUnreadMeta = jiraMetaPrefix + "inbox_unread"
)

// inboxSplitMin is the narrowest body that shows a thread beside the list.
const inboxSplitMin = 90

// inboxData is every site's threads and their marks, kept while the app
// runs.
type inboxData struct {
	threads []inboxThread
	marks   map[string]inboxMark // by thread id, nil until read from the store
	floor   time.Time
	loaded  bool
	syncing bool
	seq     int
	err     string
}

// inboxMark is a thread's marks, Unix milliseconds, 0 for none.
type inboxMark struct {
	Read   int64 `json:"r,omitempty"` // news up to then is read
	Done   int64 `json:"d,omitempty"` // done, until news after
	Snooze int64 `json:"s,omitempty"` // out of the way until then
}

// inboxThread is an issue and what others did on it in the window.
type inboxThread struct {
	jira.InboxIssue
	site, url string
	entries   []jira.InboxEntry // oldest first; none: only yours
}

func (t inboxThread) id() string { return t.site + "/" + t.Key }

func (t inboxThread) latest() time.Time {
	if len(t.entries) == 0 {
		return time.Time{}
	}
	return t.entries[len(t.entries)-1].When
}

// forYou is whether an entry after after mentions you or assigns you.
func (t inboxThread) forYou(after time.Time) bool {
	return slices.ContainsFunc(t.entries, func(e jira.InboxEntry) bool { return (e.Mention || e.Assigned) && e.When.After(after) })
}

func (t inboxThread) mentioned() bool { return t.forYou(time.Time{}) }

func ms(t time.Time) int64 { return t.UnixMilli() }

// inboxState is the thread status marks give: where the read news ends,
// and whether it is unread, done or snoozed.
type inboxState struct {
	readTo                time.Time
	unread, done, snoozed bool
	snoozeTill            time.Time
}

func (m *Model) inboxStateOf(t inboxThread, now time.Time) inboxState {
	mk := m.inboxMark(t.id())
	st := inboxState{readTo: m.inboxFloor()}
	if mk.Read != 0 {
		st.readTo = time.UnixMilli(mk.Read)
	}
	st.unread = t.latest().After(st.readTo)
	st.done = mk.Done != 0 && !t.latest().After(time.UnixMilli(mk.Done))
	if mk.Snooze > ms(now) {
		st.snoozed, st.snoozeTill = true, time.UnixMilli(mk.Snooze)
	}
	return st
}

func (m *Model) inboxData() *inboxData {
	if m.inbox == nil {
		m.inbox = &inboxData{}
	}
	return m.inbox
}

// inboxMark is thread id's marks, read from the store the first time.
func (m *Model) inboxMark(id string) inboxMark {
	d := m.inboxData()
	if d.marks == nil {
		d.marks = map[string]inboxMark{}
		if m.store != nil {
			if raw, ok, _ := m.store.GetMeta(inboxMarksMeta); ok {
				_ = json.Unmarshal([]byte(raw), &d.marks)
			}
		}
	}
	return d.marks[id]
}

// putInboxMark keeps id's marks, dropping those older than the window.
func (m *Model) putInboxMark(id string, mk inboxMark) {
	m.inboxMark(id)
	d := m.inboxData()
	d.marks[id] = mk
	old := ms(time.Now().Add(-m.opts.inboxLookback))
	maps.DeleteFunc(d.marks, func(_ string, mk inboxMark) bool { return max(mk.Read, mk.Done, mk.Snooze) < old })
	if m.store != nil {
		raw, _ := json.Marshal(d.marks)
		_ = m.store.SetMeta(inboxMarksMeta, string(raw))
	}
	m.setInboxUnread(m.inboxUnreadCount())
}

// inboxFloor is when news starts counting as unread: the first time the
// inbox was read, a day before.
func (m *Model) inboxFloor() time.Time {
	d := m.inboxData()
	if !d.floor.IsZero() {
		return d.floor
	}
	d.floor = time.Now().Add(-24 * time.Hour)
	if m.store != nil {
		if v, ok, _ := m.store.GetMeta(inboxFloorMeta); ok {
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
				d.floor = time.Unix(sec, 0)
				return d.floor
			}
		}
		_ = m.store.SetMeta(inboxFloorMeta, strconv.FormatInt(d.floor.Unix(), 10))
	}
	return d.floor
}

// inboxUnreadCount is how many threads are unread and neither done nor
// snoozed: the header's ✉.
func (m *Model) inboxUnreadCount() int {
	now, n := time.Now(), 0
	for _, t := range m.inboxData().threads {
		if st := m.inboxStateOf(t, now); len(t.entries) > 0 && st.unread && !st.done && !st.snoozed {
			n++
		}
	}
	return n
}

// inboxThread is the thread id, false when it is gone.
func (m *Model) inboxThread(id string) (inboxThread, bool) {
	ts := m.inboxData().threads
	if i := slices.IndexFunc(ts, func(t inboxThread) bool { return t.id() == id }); i >= 0 {
		return ts[i], true
	}
	return inboxThread{}, false
}

type inboxSyncMsg struct {
	seq     int
	threads []inboxThread
	err     error
}

// syncInbox reads every site's threads again, reusing those not updated
// since; none while one is on its way.
func (m *Model) syncInbox() tea.Cmd {
	d := m.inboxData()
	if !m.jiraClient.Enabled() || d.syncing {
		return nil
	}
	d.syncing = true
	d.seq++
	sites := map[string]*jira.Client{m.site: m.jiraClient}
	maps.Copy(sites, m.others())
	seq, prev, ctx, since := d.seq, d.threads, m.ctx, time.Now().Add(-m.opts.inboxLookback)
	return func() tea.Msg {
		threads, err := inboxThreads(ctx, sites, m.site, since, prev)
		return inboxSyncMsg{seq: seq, threads: threads, err: err}
	}
}

func (m Model) handleInboxSync(msg inboxSyncMsg) (tea.Model, tea.Cmd) {
	d := m.inboxData()
	if msg.seq != d.seq {
		return m, nil
	}
	d.syncing = false
	if msg.err != nil {
		d.err = msg.err.Error() // the threads before stay
		return m, nil
	}
	first := !d.loaded
	d.err, d.threads, d.loaded = "", msg.threads, true
	m.setInboxUnread(m.inboxUnreadCount())
	if m.jiraTab.inbox != nil {
		m.buildInboxRows()
		m.readInboxRow()
	}
	if first && m.jiraPicker.active && m.jiraPicker.kind == jiraPickHome && m.jiraPicker.idx == 0 {
		return m, tea.Batch(m.notifyMentions(), m.openHome()) // home opened before the inbox was in
	}
	return m, m.notifyMentions()
}

// notifyMentions raises a desktop notification for each mention newer than
// the last notified; mentions from before the app started stay quiet.
func (m *Model) notifyMentions() tea.Cmd {
	if m.mentionsSeen.IsZero() {
		m.mentionsSeen = m.started
	}
	var cmds []tea.Cmd
	newest := m.mentionsSeen
	for _, t := range m.inboxData().threads {
		for _, e := range t.entries {
			if !e.Mention || !e.When.After(m.mentionsSeen) {
				continue
			}
			newest = maxTime(newest, e.When)
			cmds = append(cmds, tea.Raw(rules.NotifySeq(e.Who+" mentioned you on "+t.Key, t.Summary)))
		}
	}
	m.mentionsSeen = newest
	return tea.Batch(cmds...)
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

type inboxTickMsg struct{}

// inboxTick syncs the inbox every ui.inbox_every; none when that is off.
func (m *Model) inboxTick() tea.Cmd {
	if m.opts.inboxEvery <= 0 {
		return nil
	}
	return tea.Tick(m.opts.inboxEvery, func(time.Time) tea.Msg { return inboxTickMsg{} })
}

func (m Model) handleInboxTick() (tea.Model, tea.Cmd) {
	return m, tea.Batch(m.syncInbox(), m.inboxTick())
}

// inboxBadge is the header's "✉ 3", "" with nothing unread.
func (m *Model) inboxBadge() string {
	if m.inboxUnread == 0 {
		return ""
	}
	return fmt.Sprintf("✉ %d", m.inboxUnread)
}

// setInboxUnread sets the header's count and keeps it for laneway prompt.
func (m *Model) setInboxUnread(n int) {
	if n == m.inboxUnread {
		return
	}
	m.inboxUnread = n
	if m.store != nil {
		_ = m.store.SetMeta(inboxUnreadMeta, strconv.Itoa(n))
	}
}

// The screen.

const (
	inboxTabInbox = iota // neither done nor snoozed
	inboxTabMentions
	inboxTabAll
	inboxTabs
)

var inboxTabNames = [inboxTabs]string{"Inbox", "Mentions", "All"}

type inboxScreen struct {
	tab  int
	rows []string // thread ids in list order, kept until built again
	row  int
	top  int // the first list line shown
	// newFrom is where a thread's news started when it was first shown,
	// so what was new stays marked after reading it.
	newFrom   map[string]time.Time
	detailTop int
	detailFor string // the thread detailTop is for
	listW     int    // as last drawn, for a click
}

// openInbox swaps the board for the inbox: the threads kept, synced again.
func (m *Model) openInbox() tea.Cmd {
	m.jiraTab.inbox = &inboxScreen{newFrom: map[string]time.Time{}}
	m.focus = focusJira
	m.buildInboxRows()
	m.readInboxRow()
	return m.syncInbox()
}

// buildInboxRows lists the tab's threads: news for you first, then the
// unread, then the rest, each newest first. The cursor stays on its thread.
func (m *Model) buildInboxRows() {
	s := m.jiraTab.inbox
	keep := s.cursorID()
	now := time.Now()
	type row struct {
		id   string
		rank int
		at   time.Time
	}
	var rows []row
	for _, t := range m.inboxData().threads {
		st := m.inboxStateOf(t, now)
		if len(t.entries) == 0 ||
			s.tab == inboxTabInbox && (st.done || st.snoozed) ||
			s.tab == inboxTabMentions && (st.done || !t.mentioned()) {
			continue
		}
		rank := 2
		switch {
		case st.done || st.snoozed:
			rank = 3
		case st.unread && t.forYou(st.readTo):
			rank = 0
		case st.unread:
			rank = 1
		}
		rows = append(rows, row{t.id(), rank, t.latest()})
	}
	slices.SortStableFunc(rows, func(a, b row) int { return cmp.Or(cmp.Compare(a.rank, b.rank), b.at.Compare(a.at)) })
	s.rows = s.rows[:0]
	for _, r := range rows {
		s.rows = append(s.rows, r.id)
	}
	if i := slices.Index(s.rows, keep); i >= 0 {
		s.row = i
	}
	s.row = min(s.row, max(len(s.rows)-1, 0))
}

func (s *inboxScreen) cursorID() string {
	if s.row < len(s.rows) {
		return s.rows[s.row]
	}
	return ""
}

// inboxSplit is whether the thread shows beside the list, and so is read
// by moving onto it.
func (m *Model) inboxSplit() bool { return m.jiraTab.view.Width() >= inboxSplitMin }

// readInboxRow marks the cursor's thread read when it shows in full.
func (m *Model) readInboxRow() {
	if m.inboxSplit() {
		m.readInboxThread(m.jiraTab.inbox.cursorID())
	}
}

// readInboxThread marks thread id read, keeping where its news started.
func (m *Model) readInboxThread(id string) {
	t, ok := m.inboxThread(id)
	if !ok {
		return
	}
	st := m.inboxStateOf(t, time.Now())
	if !st.unread {
		return
	}
	if s := m.jiraTab.inbox; s != nil {
		if _, ok := s.newFrom[id]; !ok {
			s.newFrom[id] = st.readTo
		}
	}
	mk := m.inboxMark(id)
	mk.Read = ms(t.latest())
	m.putInboxMark(id, mk)
}

// moveInbox moves the cursor d threads, the thread's top shown.
func (m *Model) moveInbox(d int) {
	s := m.jiraTab.inbox
	s.row = min(max(s.row+d, 0), max(len(s.rows)-1, 0))
	m.readInboxRow()
}

// snoozeTill is the next workday's start.
func (m *Model) snoozeTill(now time.Time) time.Time {
	y, mo, d := nextWorkday(now, m.opts.workdays).Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, time.Local).Add(m.opts.workdayStart)
}

// inboxDrop takes the cursor's thread off the list, unless All shows it.
func (m *Model) inboxDrop() {
	s := m.jiraTab.inbox
	if s.tab == inboxTabAll {
		m.buildInboxRows()
		return
	}
	s.rows = slices.Delete(s.rows, s.row, s.row+1)
	s.row = min(s.row, max(len(s.rows)-1, 0))
	m.readInboxRow()
}

func (m Model) handleInboxKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.inbox
	k := m.keys
	now := time.Now()
	t, ok := m.inboxThread(s.cursorID())
	needThread := func() bool {
		if !ok {
			m.status = "no thread here"
		}
		return ok
	}
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, k.Quit), key.Matches(msg, k.Inbox):
		m.jiraTab.inbox = nil
		m.renderJira()
	case key.Matches(msg, k.Up):
		m.moveInbox(-1)
	case key.Matches(msg, k.Down):
		m.moveInbox(1)
	case key.Matches(msg, k.Home):
		m.moveInbox(-len(s.rows))
	case key.Matches(msg, k.End):
		m.moveInbox(len(s.rows))
	case key.Matches(msg, k.PageDown):
		s.detailTop += max(m.jiraTab.view.Height()/2, 1)
	case key.Matches(msg, k.PageUp):
		s.detailTop = max(s.detailTop-max(m.jiraTab.view.Height()/2, 1), 0)
	case key.Matches(msg, k.Tab), key.Matches(msg, k.ShiftTab):
		d := 1
		if key.Matches(msg, k.ShiftTab) {
			d = inboxTabs - 1
		}
		s.tab, s.row, s.top = (s.tab+d)%inboxTabs, 0, 0
		m.buildInboxRows()
		m.readInboxRow()
	case key.Matches(msg, k.OpenChannel):
		if !needThread() {
			break
		}
		m.readInboxThread(t.id())
		if t.site != m.site {
			m.status = "opening " + t.url + "…"
			return m, m.openOpenable(openable{name: t.Key, url: t.url})
		}
		return m.openJiraKey(t.Key)
	case key.Matches(msg, k.OpenAttach):
		if needThread() {
			m.status = "opening " + t.url + "…"
			return m, m.openOpenable(openable{name: t.Key, url: t.url})
		}
	case key.Matches(msg, k.InboxDone):
		if !needThread() {
			break
		}
		mk := m.inboxMark(t.id())
		if m.inboxStateOf(t, now).done {
			mk.Done = 0
			m.status = t.Key + " back in the inbox"
		} else {
			mk.Done, mk.Snooze = ms(t.latest()), 0
			mk.Read = max(mk.Read, ms(t.latest()))
			m.status = t.Key + " done · until something new happens · " + helpKey(k.InboxDone) + " in All brings it back"
		}
		m.putInboxMark(t.id(), mk)
		m.inboxDrop()
	case key.Matches(msg, k.InboxDoneAll):
		n := 0
		for _, id := range s.rows {
			t, ok := m.inboxThread(id)
			if st := m.inboxStateOf(t, now); !ok || st.unread || st.done {
				continue
			}
			mk := m.inboxMark(id)
			mk.Done = ms(t.latest())
			m.putInboxMark(id, mk)
			n++
		}
		m.status = plural(n, "read thread") + " done"
		m.buildInboxRows()
		m.readInboxRow()
	case key.Matches(msg, k.InboxUnread):
		if !needThread() {
			break
		}
		mk := m.inboxMark(t.id())
		if m.inboxStateOf(t, now).unread {
			mk.Read = ms(t.latest())
			m.status = t.Key + " read"
		} else {
			mk.Read = ms(t.latest()) - 1 // the newest entry unread again
			delete(s.newFrom, t.id())
			m.status = t.Key + " unread · it stays so until you move off it and back"
		}
		m.putInboxMark(t.id(), mk)
	case key.Matches(msg, k.InboxSnooze):
		if !needThread() {
			break
		}
		mk := m.inboxMark(t.id())
		if m.inboxStateOf(t, now).snoozed {
			mk.Snooze = 0
			m.status = t.Key + " back in the inbox"
		} else {
			till := m.snoozeTill(now)
			mk.Snooze = ms(till)
			m.status = t.Key + " snoozed till " + inboxWhen(till, now)
		}
		m.putInboxMark(t.id(), mk)
		m.inboxDrop()
	case key.Matches(msg, k.JiraComment), key.Matches(msg, k.JiraReply):
		if !needThread() {
			break
		}
		if t.site != m.site {
			m.status = "on " + t.site + ": " + helpKey(k.OpenAttach) + " opens it in the browser"
			break
		}
		if key.Matches(msg, k.JiraComment) {
			m.openJiraCommentInputFor(t.Key)
			break
		}
		i := len(t.entries) - 1
		for i >= 0 && t.entries[i].CommentID == "" {
			i--
		}
		if i < 0 {
			m.status = "no comments to reply to"
			break
		}
		c := t.entries[i]
		m.openJiraReplyFor(t.Key, jira.Comment{ID: c.CommentID, Author: c.Who, AuthorID: c.WhoID, Body: c.Body, Created: c.When})
	case key.Matches(msg, k.Refresh):
		return m, m.syncInbox()
	case key.Matches(msg, k.CopyKey):
		if needThread() {
			m.status = t.Key + " copied"
			return m, tea.SetClipboard(t.Key)
		}
	case key.Matches(msg, k.Help):
		m.openHelp("Inbox")
	}
	return m, nil
}

// clickInbox puts the cursor on the thread clicked; a double click opens it.
func (m Model) clickInbox(x, y, count int) (tea.Model, tea.Cmd) {
	s := m.jiraTab.inbox
	line := y - jiraBodyTop
	if line < 0 || x > s.listW+1 {
		return m, nil
	}
	i := (s.top + line) / 2
	if i >= len(s.rows) {
		return m, nil
	}
	m.focus = focusJira
	s.row = i
	m.readInboxRow()
	if count >= 2 {
		return m.handleInboxKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	return m, nil
}

// inboxViewLine is the view line: the tabs and their counts, the unread and
// the keys.
func (m *Model) inboxViewLine() string {
	s, d := m.jiraTab.inbox, m.inboxData()
	now := time.Now()
	var counts [inboxTabs]int
	for _, t := range d.threads {
		st := m.inboxStateOf(t, now)
		if len(t.entries) == 0 {
			continue
		}
		counts[inboxTabAll]++
		if !st.done && !st.snoozed {
			counts[inboxTabInbox]++
		}
		if !st.done && t.mentioned() {
			counts[inboxTabMentions]++
		}
	}
	var tabs []string
	for i, name := range inboxTabNames {
		label := fmt.Sprintf("%s %d", name, counts[i])
		if i == s.tab {
			tabs = append(tabs, jiraViewActive.Render(label))
		} else {
			tabs = append(tabs, jiraDimStyle.Render(label))
		}
	}
	line := strings.Join(tabs, jiraDimStyle.Render(" · "))
	if m.inboxUnread > 0 {
		line += jiraDimStyle.Render(fmt.Sprintf("  ·  %d unread", m.inboxUnread))
	}
	if d.syncing {
		line += jiraDimStyle.Render(" · syncing…")
	}
	k := m.keys
	keys := fmt.Sprintf("  ·  %s view · %s open · %s comment · %s reply · %s done · %s unread · %s snooze · %s browser · esc board",
		helpKey(k.Tab), helpKey(k.OpenChannel), helpKey(k.JiraComment), helpKey(k.JiraReply), helpKey(k.InboxDone),
		helpKey(k.InboxUnread), helpKey(k.InboxSnooze), helpKey(k.OpenAttach))
	return line + jiraDimStyle.Render(keys)
}

// renderInbox draws the thread list and, when wide enough, the cursor's
// thread beside it.
func (m *Model) renderInbox(width, height int) string {
	s, d := m.jiraTab.inbox, m.inboxData()
	switch {
	case d.err != "" && !d.loaded:
		out, _ := jiraErrorState(d.err, width, height, m.screenErrHints()...)
		return out
	case !d.loaded:
		return refDimStyle.Render("  reading your issues' news…")
	case len(s.rows) == 0:
		return m.renderInboxEmpty()
	}
	listW := width
	if m.inboxSplit() {
		listW = min(max(width*2/5, 36), 64)
	}
	s.listW = listW
	list := m.renderInboxList(listW, height)
	if listW == width {
		return strings.Join(list, "\n")
	}
	detail := m.renderInboxThread(width-listW-3, height)
	out := make([]string, height)
	for i := range out {
		l, r := "", ""
		if i < len(list) {
			l = list[i]
		}
		if i < len(detail) {
			r = detail[i]
		}
		out[i] = l + strings.Repeat(" ", max(listW-ansi.StringWidth(l), 0)) + refDimStyle.Render(" │ ") + r
	}
	return strings.Join(out, "\n")
}

// renderInboxEmpty says there is nothing, and what is out of the way.
func (m *Model) renderInboxEmpty() string {
	s := m.jiraTab.inbox
	now := time.Now()
	done, snoozed := 0, 0
	for _, t := range m.inboxData().threads {
		st := m.inboxStateOf(t, now)
		if len(t.entries) > 0 && st.done {
			done++
		} else if len(t.entries) > 0 && st.snoozed {
			snoozed++
		}
	}
	msg := "nothing new since " + inboxWhen(now.Add(-m.opts.inboxLookback), now)
	if s.tab == inboxTabMentions {
		msg = "no one mentioned you"
	}
	if m.opts.delight && s.tab == inboxTabInbox {
		msg += " · all caught up ✓"
	}
	out := []string{"", "  " + msg}
	if s.tab != inboxTabAll && done+snoozed > 0 {
		out = append(out, "", refDimStyle.Render(fmt.Sprintf("  %d done, %d snoozed · %s shows All", done, snoozed, helpKey(m.keys.Tab))))
	}
	return strings.Join(out, "\n")
}

// renderInboxList draws two lines a thread: its marks, key, summary and
// when; then its status and the newest news.
func (m *Model) renderInboxList(width, height int) []string {
	s := m.jiraTab.inbox
	now := time.Now()
	var out []string
	for i, id := range s.rows {
		t, _ := m.inboxThread(id)
		st := m.inboxStateOf(t, now)
		dot := " "
		switch {
		case st.done:
			dot = "✓"
		case st.snoozed:
			dot = "◷"
		case st.unread:
			dot = "●"
		}
		at := " "
		if t.mentioned() {
			at = "@"
		}
		when := inboxWhen(t.latest(), now)
		head := jiraKeyStyle.Render(t.Key) + " " + t.Summary
		if t.site != m.site {
			head = "[" + t.site + "] " + head
		}
		headW := max(width-4-ansi.StringWidth(when)-1, 8)
		head = ansi.Truncate(head, headW, "…")
		line1 := dot + " " + at + " " + head + strings.Repeat(" ", max(headW-ansi.StringWidth(head), 0)) + " " + when

		e := t.entries[len(t.entries)-1]
		news := t.Status + " · " + e.Who + ": " + e.What
		n := 0
		for _, e := range t.entries {
			if e.When.After(st.readTo) {
				n++
			}
		}
		tail := ""
		if n > 1 {
			tail = fmt.Sprintf(" %d new", n)
		}
		newsW := max(width-4-ansi.StringWidth(tail), 8)
		line2 := "    " + ansi.Truncate(news, newsW, "…")
		line2 += strings.Repeat(" ", max(width-ansi.StringWidth(line2)-ansi.StringWidth(tail), 0)) + tail

		if i == s.row {
			out = append(out, selectedRow.Render(pad(ansi.Strip(line1), width)), selectedRow.Render(pad(ansi.Strip(line2), width)))
			continue
		}
		if !st.unread || st.done || st.snoozed {
			line1 = refDimStyle.Render(ansi.Strip(line1))
		} else {
			line1 = refKeyStyle.Render(dot) + line1[len(dot):]
		}
		out = append(out, line1, refDimStyle.Render(ansi.Strip(line2)))
	}
	cursor := s.row * 2
	if cursor < s.top {
		s.top = cursor
	}
	if cursor+2 > s.top+height {
		s.top = cursor + 2 - height
	}
	s.top = min(max(s.top, 0), max(len(out)-height, 0))
	return out[s.top:min(len(out), s.top+height)]
}

func pad(s string, w int) string { return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) }

// renderInboxThread draws the cursor's thread: the issue on top, then what
// others did, oldest first, what was new marked ●. It opens on the first
// new entry.
func (m *Model) renderInboxThread(width, height int) []string {
	s := m.jiraTab.inbox
	t, ok := m.inboxThread(s.cursorID())
	if !ok || width < 10 {
		return nil
	}
	now := time.Now()
	st := m.inboxStateOf(t, now)
	newFrom, ok := s.newFrom[t.id()]
	if !ok {
		newFrom = st.readTo
	}
	head := []string{jiraKeyStyle.Render(t.Key) + " " + titleStyle.Render(ansi.Truncate(t.Summary, max(width-len(t.Key)-1, 1), "…"))}
	facts := []string{t.Status, cmp.Or(t.Assignee, "unassigned")}
	if t.site != m.site {
		facts = append(facts, "on "+t.site)
	}
	switch {
	case st.done:
		facts = append(facts, "done ✓")
	case st.snoozed:
		facts = append(facts, "snoozed till "+inboxWhen(st.snoozeTill, now))
	}
	head = append(head, refDimStyle.Render(ansi.Truncate(strings.Join(facts, " · "), width, "…")), "")

	var b strings.Builder
	first := -1 // the first new entry's line
	for i, e := range t.entries {
		if i > 0 {
			b.WriteString("\n")
		}
		fresh := e.When.After(newFrom)
		if fresh && first < 0 {
			first = strings.Count(b.String(), "\n")
		}
		by := e.Who + " · " + m.when(e.When)
		switch {
		case e.Mention:
			by += " · mentioned you"
		case e.Assigned:
			by += " · assigned you"
		case e.CommentID != "":
			by += " · commented"
		}
		if fresh {
			b.WriteString(refKeyStyle.Render("● "+by) + "\n")
		} else {
			b.WriteString(refDimStyle.Render("  "+by) + "\n")
		}
		if e.CommentID != "" {
			b.WriteString(renderMarkdown(e.Body, m.emojiImg, nil, ""))
		} else {
			var ch strings.Builder
			m.renderChangeFields(&ch, e)
			for _, l := range strings.SplitAfter(ch.String(), "\n") {
				if l != "" {
					b.WriteString("  " + l)
				}
			}
		}
	}
	body := strings.Split(strings.TrimRight(wrapPanel(b.String(), width), "\n"), "\n")
	room := max(height-len(head), 1)
	if s.detailFor != t.id() {
		s.detailFor, s.detailTop = t.id(), 0
		if first > 0 && len(body) > room {
			s.detailTop = first
		}
	}
	s.detailTop = min(s.detailTop, max(len(body)-room, 0))
	body = body[s.detailTop:min(len(body), s.detailTop+room)]
	if s.detailTop > 0 {
		body[0] = refDimStyle.Render(fmt.Sprintf("  ↑ %d earlier lines · %s", s.detailTop, helpKey(m.keys.PageUp)))
	}
	return append(head, body...)
}

// inboxWhen is "15:04" today, "Mon 15:04" this week, "2 Jan" before.
func inboxWhen(t, now time.Time) string {
	t = t.Local()
	if y, mo, d := now.Date(); t.Year() == y && t.Month() == mo && t.Day() == d {
		return t.Format("15:04")
	}
	if d := now.Sub(t); d < 6*24*time.Hour && d > -6*24*time.Hour {
		return t.Format("Mon 15:04")
	}
	return t.Format("2 Jan")
}
