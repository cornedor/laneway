package ui

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
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
