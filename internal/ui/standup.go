package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// Your standup: what you did since the previous workday, by issue, with
// your git commits (standup_screen.go shows it).

// loadMyStandup fetches your activity since since and the cards it needs.
func (m *Model) loadMyStandup(seq int, since time.Time) tea.Cmd {
	now := time.Now()
	c, ctx, repos, me := m.jiraClient, m.ctx, m.standupRepos(), m.jiraClient.KnownMyself()
	return func() tea.Msg {
		entries, err := c.Standup(ctx, since)
		if err != nil {
			return standupMsg{seq: seq, err: err}
		}
		entries = withCommits(entries, gitCommits(repos, since))
		var keys []string
		for _, e := range entries {
			if e.Key != "" && !slices.Contains(keys, e.Key) {
				keys = append(keys, e.Key)
			}
		}
		cards, cerr := c.SearchCards(ctx, standupJQL(keys))
		if cerr != nil && len(keys) > 0 {
			cards, _ = c.SearchCards(ctx, standupJQL(nil)) // a deleted key fails the whole search
		} // without cards the rows still list the activity
		lines, text := standupMine(entries, cards, me, since, now)
		return standupMsg{seq: seq, lines: lines, text: text}
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
		slices.SortStableFunc(lines, func(a, b issueLine) int { // no ticket last
			switch {
			case a.key == "" && b.key != "":
				return 1
			case a.key != "" && b.key == "":
				return -1
			}
			return 0
		})
		for _, l := range lines {
			fmt.Fprintf(&b, "- %s: %s\n", cmp.Or(strings.TrimSpace(l.key+" "+l.summary), noTicket), strings.Join(l.what, "; "))
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

// nextWorkday is the first workday after day.
func nextWorkday(day time.Time, workdays []time.Weekday) time.Time {
	if len(workdays) == 0 {
		workdays = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	}
	d := day.AddDate(0, 0, 1)
	for !slices.Contains(workdays, d.Weekday()) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}
