package ui

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// I opens the inbox: what others did on your issues since you last opened
// it (the first time, the last day). Mentions come first; enter opens one.
// Its last row brings back the one before, so an inbox opened and closed
// by accident isn't lost.

const (
	inboxMeta     = jiraMetaPrefix + "inbox_seen"
	inboxPrevMeta = jiraMetaPrefix + "inbox_prev" // the start of the inbox last read
	inboxPrevID   = "\x00previous"                // the row that reopens it
	// inboxUnreadMeta is the header's count, for laneway prompt.
	inboxUnreadMeta = jiraMetaPrefix + "inbox_unread"
)

// inboxSince is when the inbox was last read, ui.inbox_lookback (a day)
// ago the first time.
func (m *Model) inboxSince(now time.Time) time.Time {
	if m.store != nil {
		if v, ok, _ := m.store.GetMeta(inboxMeta); ok {
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
				return time.Unix(sec, 0)
			}
		}
	}
	return now.Add(-m.opts.inboxLookback)
}

// inboxPrev is when the inbox read last started, false before a second read.
func (m *Model) inboxPrev() (time.Time, bool) {
	if m.store == nil {
		return time.Time{}, false
	}
	v, ok, _ := m.store.GetMeta(inboxPrevMeta)
	sec, err := strconv.ParseInt(v, 10, 64)
	return time.Unix(sec, 0), ok && err == nil
}

// openInbox loads the entries since the last read into a picker, and marks
// them read once they are in.
func (m *Model) openInbox() tea.Cmd {
	return m.openInboxSince(m.inboxSince(time.Now()), true)
}

// openInboxSince lists the entries since since; mark moves the read marks
// on, keeping since as the start of the inbox before for the next read.
func (m *Model) openInboxSince(since time.Time, mark bool) tea.Cmd {
	now := time.Now()
	prev, hasPrev := m.inboxPrev() // the read before the one since
	gen := m.startJiraPicker(jiraPickInbox, "Inbox", true)
	seq := m.jiraPicker.fetchSeq
	c, ctx, st, others, delight := m.jiraClient, m.ctx, m.store, m.others(), m.opts.delight
	return func() tea.Msg {
		entries, err := inboxAll(ctx, c, others, since)
		items := make([]jiraPickerItem, len(entries))
		for i, e := range entries {
			at := " "
			if e.Mention {
				at = "@"
			}
			id, where := e.Key, ""
			if e.site != "" {
				id, where = siteEntryPrefix+e.url, "["+e.site+"] "
			}
			items[i] = jiraPickerItem{id: id, label: fmt.Sprintf("%s %s  %s  %s%s %s — %s", at, inboxWhen(e.When, now), e.Who, where, e.Key, e.Summary, e.What)}
		}
		if err == nil {
			if st != nil && mark {
				_ = st.SetMeta(inboxPrevMeta, strconv.FormatInt(since.Unix(), 10))
				_ = st.SetMeta(inboxMeta, strconv.FormatInt(now.Unix(), 10))
			}
			if len(items) == 0 {
				items = []jiraPickerItem{{label: "nothing new"}}
				if delight {
					items[0].label = "nothing new · all caught up ✓"
				}
			}
			if hasPrev && prev.Before(since) {
				items = append(items, jiraPickerItem{id: inboxPrevID, label: "↶ the inbox before, since " + inboxWhen(prev, now),
					value: strconv.FormatInt(prev.Unix(), 10)}) // this read moves the stored one on
			}
		}
		issues := map[string]bool{} // as the header's ✉ counts them
		for _, e := range entries {
			issues[e.site+" "+e.Key] = true
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickInbox, items: items, err: err,
			title: fmt.Sprintf("Inbox — %s since %s", inboxCount(len(entries), len(issues)), inboxWhen(since, now))}
	}
}

// inboxCount is "1 change", "5 changes on 2 issues".
func inboxCount(changes, issues int) string {
	switch {
	case changes == 1:
		return "1 change"
	case issues == 1:
		return fmt.Sprintf("%d changes on 1 issue", changes)
	}
	return fmt.Sprintf("%d changes on %d issues", changes, issues)
}

// inboxWhen is "15:04" today, "Mon 15:04" before.
func inboxWhen(t, now time.Time) string {
	t = t.Local()
	if y, mo, d := now.Date(); t.Year() == y && t.Month() == mo && t.Day() == d {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

type inboxTickMsg struct{}

type inboxCountMsg struct{ n int }

// inboxTick refreshes the header's unread count every ui.inbox_every;
// none when that is off.
func (m *Model) inboxTick() tea.Cmd {
	if m.opts.inboxEvery <= 0 {
		return nil
	}
	return tea.Tick(m.opts.inboxEvery, func(time.Time) tea.Msg { return inboxTickMsg{} })
}

// countInbox asks how many issues the inbox would show, for the header.
func (m *Model) countInbox() tea.Cmd {
	if !m.jiraClient.Enabled() {
		return nil
	}
	c, ctx, since, others := m.jiraClient, m.ctx, m.inboxSince(time.Now()), m.others()
	return func() tea.Msg {
		n, err := inboxCountAll(ctx, c, others, since)
		if err != nil {
			return nil // a badge is not worth an error line
		}
		return inboxCountMsg{n}
	}
}

func (m Model) handleInboxTick() (tea.Model, tea.Cmd) {
	return m, tea.Batch(m.countInbox(), m.inboxTick())
}

// inboxBadge is the header's "✉ 3", "" with nothing new.
func (m *Model) inboxBadge() string {
	if m.inboxUnread == 0 {
		return ""
	}
	return fmt.Sprintf("✉ %d", m.inboxUnread)
}

// setInboxUnread sets the header's count and keeps it for laneway prompt.
func (m *Model) setInboxUnread(n int) {
	m.inboxUnread = n
	if m.store != nil {
		_ = m.store.SetMeta(inboxUnreadMeta, strconv.Itoa(n))
	}
}

// inboxMentionsMsg are the inbox's mentions, read when the count rose.
type inboxMentionsMsg struct{ entries []jira.InboxEntry }

// handleInboxCount shows the count; when it rose, reads the inbox for
// mentions to notify.
func (m Model) handleInboxCount(msg inboxCountMsg) (tea.Model, tea.Cmd) {
	rose := msg.n > m.inboxUnread
	m.setInboxUnread(msg.n)
	if !rose {
		return m, nil
	}
	c, ctx, since := m.jiraClient, m.ctx, m.inboxSince(time.Now())
	return m, func() tea.Msg {
		entries, err := c.Inbox(ctx, since)
		if err != nil {
			return nil
		}
		return inboxMentionsMsg{entries}
	}
}

// handleInboxMentions notifies each mention newer than the last notified;
// mentions from before the app started stay quiet.
func (m Model) handleInboxMentions(msg inboxMentionsMsg) (tea.Model, tea.Cmd) {
	if m.mentionsSeen.IsZero() {
		m.mentionsSeen = m.started
	}
	var cmds []tea.Cmd
	newest := m.mentionsSeen
	for _, e := range msg.entries {
		if !e.Mention || !e.When.After(m.mentionsSeen) {
			continue
		}
		if e.When.After(newest) {
			newest = e.When
		}
		cmds = append(cmds, tea.Raw(rules.NotifySeq(e.Who+" mentioned you on "+e.Key, e.Summary)))
	}
	m.mentionsSeen = newest
	return m, tea.Batch(cmds...)
}
