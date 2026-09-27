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

// U lists what you did since the previous workday, by day: moves, edits,
// comments and logged work. Its first row copies it as text for a standup.

// openStandup loads your activity into a picker.
func (m *Model) openStandup() tea.Cmd {
	return m.openStandupSince(jira.PreviousWorkday(time.Now(), m.opts.workdays), false)
}

// openStandupSince loads your activity since since, or the board's
// people's with team; U again inside it reaches a workday further back.
func (m *Model) openStandupSince(since time.Time, team bool) tea.Cmd {
	if team {
		return m.openTeamStandup(since)
	}
	now := time.Now()
	gen := m.startJiraPicker(jiraPickStandup, "Standup", true)
	m.jiraPicker.day = since
	seq := m.jiraPicker.fetchSeq
	c, ctx, repos := m.jiraClient, m.ctx, m.standupRepos()
	// Rows step a workday either way, U back too.
	steps := m.standupSteps(since, now)
	if len(m.teamPeople()) > 0 {
		steps = append(steps, jiraPickerItem{id: "team", label: "Team: everyone on the board"})
	}
	return func() tea.Msg {
		entries, err := c.Standup(ctx, since)
		if err == nil {
			entries = withCommits(entries, gitCommits(repos, since))
		}
		text := standupText(entries)
		items := append([]jiraPickerItem{{id: "copy", label: "Copy as text"}}, steps...)
		day := ""
		for _, e := range entries {
			if d := standupDay(e.When, now); d != day {
				day = d
				items = append(items, jiraPickerItem{label: "── " + d})
			}
			what := strings.TrimSpace(e.Key + " " + e.Summary)
			items = append(items, jiraPickerItem{id: e.Key, label: fmt.Sprintf("  %s  %s — %s", e.When.Local().Format("15:04"), cmp.Or(what, noTicket), e.What)})
		}
		if err == nil && len(entries) == 0 {
			items = append([]jiraPickerItem{{label: "nothing since " + standupDay(since, now)}}, steps...)
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStandup, items: items, err: err,
			title: "Standup — since " + standupDay(since, now) + "  ·  U further back", text: text}
	}
}

// standupSteps are the rows stepping a workday back, and forth when since
// is before the last.
func (m *Model) standupSteps(since, now time.Time) []jiraPickerItem {
	steps := []jiraPickerItem{{id: "earlier", label: "← a workday further back"}}
	if since.Before(jira.PreviousWorkday(now, m.opts.workdays)) {
		steps = append(steps, jiraPickerItem{id: "later", label: "→ a workday later"})
	}
	return steps
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
