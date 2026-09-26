package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// U lists what you did since the previous workday, by day: moves, edits,
// comments and logged work. Its first row copies it as text for a standup.

// openStandup loads your activity into a picker.
func (m *Model) openStandup() tea.Cmd {
	return m.openStandupSince(jira.PreviousWorkday(time.Now()))
}

// openStandupSince loads your activity since since; U again inside it
// reaches a workday further back.
func (m *Model) openStandupSince(since time.Time) tea.Cmd {
	now := time.Now()
	gen := m.startJiraPicker(jiraPickStandup, "Standup", true)
	m.jiraPicker.day = since
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		entries, err := c.Standup(ctx, since)
		text := standupText(entries)
		items := []jiraPickerItem{{id: "copy", label: "Copy as text"}}
		day := ""
		for _, e := range entries {
			if d := standupDay(e.When, now); d != day {
				day = d
				items = append(items, jiraPickerItem{label: "── " + d})
			}
			items = append(items, jiraPickerItem{id: e.Key, label: fmt.Sprintf("  %s  %s %s — %s", e.When.Local().Format("15:04"), e.Key, e.Summary, e.What)})
		}
		if err == nil && len(entries) == 0 {
			items = []jiraPickerItem{{label: "nothing since " + standupDay(since, now)}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStandup, items: items, err: err,
			title: "Standup — since " + standupDay(since, now) + "  ·  U further back", text: text}
	}
}

// standupDay names t's day: Today, Yesterday, else the weekday and date.
func standupDay(t, now time.Time) string {
	t = t.Local()
	y, mo, d := now.Date()
	switch {
	case t.Year() == y && t.Month() == mo && t.Day() == d:
		return "Today"
	case t.Format(time.DateOnly) == now.AddDate(0, 0, -1).Format(time.DateOnly):
		return "Yesterday"
	}
	return t.Format("Mon 2 Jan")
}

// standupText is the activity as plain lines, one issue once per day:
// "Today\n- ABC-1 Fix login: status: To Do → Done; logged 1h".
func standupText(entries []jira.InboxEntry) string {
	now := time.Now()
	var b strings.Builder
	day := ""
	type issueLine struct {
		key, summary string
		what         []string
	}
	var lines []issueLine
	flush := func() {
		for _, l := range lines {
			fmt.Fprintf(&b, "- %s %s: %s\n", l.key, l.summary, strings.Join(l.what, "; "))
		}
		lines = nil
	}
	for _, e := range entries {
		if d := standupDay(e.When, now); d != day {
			flush()
			if day != "" {
				b.WriteString("\n")
			}
			day = d
			b.WriteString(d + "\n")
		}
		found := false
		for i := range lines {
			if lines[i].key == e.Key {
				lines[i].what = append(lines[i].what, e.What)
				found = true
			}
		}
		if !found {
			lines = append(lines, issueLine{e.Key, e.Summary, []string{e.What}})
		}
	}
	flush()
	return strings.TrimSpace(b.String())
}
