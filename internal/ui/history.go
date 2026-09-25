package ui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// H in the panel: the issue's history, newest first — who changed what, and
// the comments in between. Typing filters it.

func (m *Model) openHistory() tea.Cmd {
	if m.jiraIssue == nil {
		return nil
	}
	key := m.jiraIssue.Key
	gen := m.startJiraPicker(jiraPickHistory, "History — "+key, true)
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		entries, err := c.History(ctx, key)
		now := time.Now()
		items := make([]jiraPickerItem, len(entries))
		for i, e := range entries {
			items[i] = jiraPickerItem{label: fmt.Sprintf("%s  %s  %s", inboxWhen(e.When, now), orDash(e.Who), e.What)}
		}
		if err == nil && len(items) == 0 {
			items = []jiraPickerItem{{label: "no history yet"}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickHistory, items: items, err: err}
	}
}
