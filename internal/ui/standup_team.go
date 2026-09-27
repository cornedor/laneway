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

// The standup's Team row: what each person on the board's cards did since
// the day it starts — moves, comments and logged work, with the time each
// logged — for whoever runs the standup.

// teamPerson is one of the board's people.
type teamPerson struct{ id, name string }

// teamPeople are the assignees of the board's cards, by name.
func (m *Model) teamPeople() []teamPerson {
	var out []teamPerson
	for _, c := range m.jiraTab.cards {
		if c.AssigneeID != "" && !slices.ContainsFunc(out, func(p teamPerson) bool { return p.id == c.AssigneeID }) {
			out = append(out, teamPerson{c.AssigneeID, c.Assignee})
		}
	}
	slices.SortFunc(out, func(a, b teamPerson) int { return strings.Compare(a.name, b.name) })
	return out
}

// openTeamStandup loads the board's people's activity since since.
func (m *Model) openTeamStandup(since time.Time) tea.Cmd {
	now := time.Now()
	people := m.teamPeople()
	gen := m.startJiraPicker(jiraPickStandup, "Team standup", true)
	m.jiraPicker.day, m.jiraPicker.team = since, true
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	ids := make([]string, len(people))
	for i, p := range people {
		ids[i] = p.id
	}
	steps := append(m.standupSteps(since, now), jiraPickerItem{id: "me", label: "Just me"})
	return func() tea.Msg {
		entries, err := c.TeamStandup(ctx, since, ids)
		items := append([]jiraPickerItem{{id: "copy", label: "Copy as text"}}, steps...)
		var text []string
		for _, p := range people {
			var theirs []jira.InboxEntry
			for _, e := range entries {
				if e.Who == p.name {
					theirs = append(theirs, e)
				}
			}
			head := p.name + teamLogged(theirs)
			items = append(items, jiraPickerItem{label: "── " + head})
			if len(theirs) == 0 {
				items = append(items, jiraPickerItem{label: "  nothing since " + standupDay(since, now)})
				text = append(text, head+"\nnothing")
				continue
			}
			for _, e := range theirs {
				what := strings.TrimSpace(e.Key + " " + e.Summary)
				items = append(items, jiraPickerItem{id: e.Key, label: fmt.Sprintf("  %s  %s — %s",
					teamWhen(e.When, now), cmp.Or(what, noTicket), e.What)})
			}
			text = append(text, head+"\n"+standupText(theirs))
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStandup, items: items, err: err,
			title: fmt.Sprintf("Team standup — %d people since %s  ·  U further back", len(people), standupDay(since, now)),
			text:  strings.Join(text, "\n\n")}
	}
}

// teamLogged is " · logged 6h" of the entries' worklogs, "" for none.
func teamLogged(entries []jira.InboxEntry) string {
	secs := 0
	for _, e := range entries {
		secs += e.Logged
	}
	if secs == 0 {
		return ""
	}
	return " · logged " + jira.FormatDuration(secs)
}

// teamWhen is "15:04" today, "Fri 15:04" before.
func teamWhen(t, now time.Time) string {
	if standupDay(t, now) == "Today" {
		return t.Local().Format("15:04")
	}
	return t.Local().Format("Mon 15:04")
}
