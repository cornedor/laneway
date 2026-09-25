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

const inboxMeta = jiraMetaPrefix + "inbox_seen"

// inboxSince is when the inbox was last read, a day ago the first time.
func (m *Model) inboxSince(now time.Time) time.Time {
	if m.store != nil {
		if v, ok, _ := m.store.GetMeta(inboxMeta); ok {
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
				return time.Unix(sec, 0)
			}
		}
	}
	return now.Add(-24 * time.Hour)
}

// openInbox loads the entries since the last read into a picker, and marks
// them read once they are in.
func (m *Model) openInbox() tea.Cmd {
	now := time.Now()
	since := m.inboxSince(now)
	m.inboxUnread = 0
	gen := m.startJiraPicker(jiraPickInbox, "Inbox", true)
	seq := m.jiraPicker.fetchSeq
	c, ctx, st := m.jiraClient, m.ctx, m.store
	return func() tea.Msg {
		entries, err := c.Inbox(ctx, since)
		items := make([]jiraPickerItem, len(entries))
		for i, e := range entries {
			mark := " "
			if e.Mention {
				mark = "@"
			}
			items[i] = jiraPickerItem{id: e.Key, label: fmt.Sprintf("%s %s  %s  %s %s — %s", mark, inboxWhen(e.When, now), e.Who, e.Key, e.Summary, e.What)}
		}
		if err == nil {
			if st != nil {
				_ = st.SetMeta(inboxMeta, strconv.FormatInt(now.Unix(), 10))
			}
			if len(items) == 0 {
				items = []jiraPickerItem{{label: "nothing new"}}
			}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickInbox, items: items, err: err,
			title: fmt.Sprintf("Inbox — %d since %s", len(entries), inboxWhen(since, now))}
	}
}

// inboxWhen is "15:04" today, "Mon 15:04" before.
func inboxWhen(t, now time.Time) string {
	t = t.Local()
	if y, mo, d := now.Date(); t.Year() == y && t.Month() == mo && t.Day() == d {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

// inboxEvery is how often the header's unread count is refreshed.
const inboxEvery = 5 * time.Minute

type inboxTickMsg struct{}

type inboxCountMsg struct{ n int }

func inboxTick() tea.Cmd {
	return tea.Tick(inboxEvery, func(time.Time) tea.Msg { return inboxTickMsg{} })
}

// countInbox asks how many issues the inbox would show, for the header.
func (m *Model) countInbox() tea.Cmd {
	if !m.jiraClient.Enabled() {
		return nil
	}
	c, ctx, since := m.jiraClient, m.ctx, m.inboxSince(time.Now())
	return func() tea.Msg {
		n, err := c.InboxCount(ctx, since)
		if err != nil {
			return nil // a badge is not worth an error line
		}
		return inboxCountMsg{n}
	}
}

func (m Model) handleInboxTick() (tea.Model, tea.Cmd) {
	return m, tea.Batch(m.countInbox(), inboxTick())
}

// inboxBadge is the header's "✉ 3", "" with nothing new.
func (m *Model) inboxBadge() string {
	if m.inboxUnread == 0 {
		return ""
	}
	return fmt.Sprintf("✉ %d", m.inboxUnread)
}

// inboxMentionsMsg are the inbox's mentions, read when the count rose.
type inboxMentionsMsg struct{ entries []jira.InboxEntry }

// handleInboxCount shows the count; when it rose, reads the inbox for
// mentions to notify.
func (m Model) handleInboxCount(msg inboxCountMsg) (tea.Model, tea.Cmd) {
	rose := msg.n > m.inboxUnread
	m.inboxUnread = msg.n
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
