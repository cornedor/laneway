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

// The standup's Team row walks the board right to left, as a team runs its
// daily scrum: closest to done first, the work rather than the people. Each
// card says who has it, how long it has been in progress, a flag, its pull
// request or deploy, and what happened on it since the day the standup
// starts, or "no activity" — the stuck card worth raising. A row groups it
// by person instead, for teams that go round.

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

// teamColumn is a board column and its cards, as the standup walks it.
type teamColumn struct {
	name  string
	cards []jira.Card
}

// teamColumns are the board's columns and their cards, in board order; a
// list without lanes is one column.
func (m *Model) teamColumns() []teamColumn {
	t := m.jiraTab
	if len(t.lanes) == 0 {
		return []teamColumn{{name: "Issues", cards: slices.Clone(t.cards)}}
	}
	out := make([]teamColumn, len(t.lanes))
	for i, l := range t.lanes {
		out[i].name = l.name
		for _, ci := range l.cards {
			out[i].cards = append(out[i].cards, t.cards[ci])
		}
	}
	return out
}

// teamHeader is the sprint goal and the workdays left, "" off a sprint.
func (m *Model) teamHeader(now time.Time) string {
	v, ok := m.jiraCurrentView()
	if !ok {
		return ""
	}
	var parts []string
	if g := strings.Join(strings.Fields(v.goal), " "); g != "" {
		parts = append(parts, "goal: "+g)
	}
	if !v.end.IsZero() && v.end.After(now) {
		parts = append(parts, plural(workdaysLeft(now, v.end, m.opts.workdays), "workday")+" left")
	}
	return strings.Join(parts, " · ")
}

// openTeamStandup loads the board's people's activity since since and walks
// the board with it, or groups it by person.
func (m *Model) openTeamStandup(since time.Time, byPerson bool) tea.Cmd {
	now := time.Now()
	people := m.teamPeople()
	gen := m.startJiraPicker(jiraPickStandup, "Team standup", true)
	m.jiraPicker.day, m.jiraPicker.team, m.jiraPicker.byPerson = since, true, byPerson
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	ids := make([]string, len(people))
	for i, p := range people {
		ids[i] = p.id
	}
	cols, head, stale := m.teamColumns(), m.teamHeader(now), m.opts.staleDays
	group := jiraPickerItem{id: "group", label: "By person"}
	if byPerson {
		group.label = "Walk the board"
	}
	steps := append(m.standupSteps(since, now), group, jiraPickerItem{id: "me", label: "Just me"})
	return func() tea.Msg {
		entries, err := c.TeamStandup(ctx, since, ids)
		rows, text := teamWalk(cols, entries, byPerson, stale, since, now)
		items := append([]jiraPickerItem{{id: "copy", label: "Copy as text"}}, steps...)
		items = append(items, rows...)
		title := "Team standup — since " + standupDay(since, now)
		if head != "" {
			title += " · " + head
			text = head + "\n\n" + text
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStandup, items: items, err: err,
			title: title + "  ·  U further back", text: text}
	}
}

// teamCard is a card of the walk and what happened on it.
type teamCard struct {
	card   jira.Card
	column string
	events []jira.InboxEntry
}

// teamWalk is the Team standup's rows and text: the columns right to left
// (or byPerson, each person's cards in that order), then activity on issues
// not on the board. A done or not started card shows only when something
// happened on it, or it is flagged; one in progress always does.
func teamWalk(cols []teamColumn, entries []jira.InboxEntry, byPerson bool, stale int, since, now time.Time) ([]jiraPickerItem, string) {
	byKey := map[string][]jira.InboxEntry{}
	for _, e := range entries {
		byKey[e.Key] = append(byKey[e.Key], e)
	}
	var walk []teamCard
	onBoard := map[string]bool{}
	for i := len(cols) - 1; i >= 0; i-- {
		for _, c := range cols[i].cards {
			onBoard[c.Key] = true
			if ev := byKey[c.Key]; len(ev) > 0 || c.InProgress || c.Flagged {
				walk = append(walk, teamCard{card: c, column: cols[i].name, events: ev})
			}
		}
	}
	var items []jiraPickerItem
	var text strings.Builder
	section := func(name string, list []teamCard, row func(teamCard) string) {
		if len(list) == 0 {
			return
		}
		items = append(items, jiraPickerItem{label: fmt.Sprintf("── %s (%d)", name, len(list))})
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(name + "\n")
		for _, tc := range list {
			line := row(tc)
			items = append(items, jiraPickerItem{id: tc.card.Key, label: "  " + line})
			text.WriteString("- " + line + "\n")
		}
	}
	if byPerson {
		var names []string
		for _, tc := range walk {
			if n := cmp.Or(tc.card.Assignee, "Unassigned"); !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		slices.SortFunc(names, func(a, b string) int {
			switch {
			case a == "Unassigned":
				return 1
			case b == "Unassigned":
				return -1
			}
			return strings.Compare(a, b)
		})
		for _, n := range names {
			var theirs []teamCard
			var events []jira.InboxEntry
			for _, tc := range walk {
				if cmp.Or(tc.card.Assignee, "Unassigned") == n {
					theirs = append(theirs, tc)
					events = append(events, tc.events...)
				}
			}
			section(n+teamLogged(events), theirs, func(tc teamCard) string { return teamRow(tc, tc.column, stale, now) })
		}
	} else {
		for i := len(cols) - 1; i >= 0; i-- {
			var in []teamCard
			for _, tc := range walk {
				if tc.column == cols[i].name {
					in = append(in, tc)
				}
			}
			section(cols[i].name, in, func(tc teamCard) string {
				return teamRow(tc, cmp.Or(tc.card.Assignee, "unassigned"), stale, now)
			})
		}
	}
	// Activity on issues the board doesn't show, by issue.
	var off []teamCard
	for _, e := range entries {
		if onBoard[e.Key] || slices.ContainsFunc(off, func(tc teamCard) bool { return tc.card.Key == e.Key && e.Key != "" }) {
			continue
		}
		off = append(off, teamCard{card: jira.Card{Key: e.Key, Summary: e.Summary}, events: byKey[e.Key]})
	}
	section("Off the board", off, func(tc teamCard) string {
		var who []string
		for _, e := range tc.events {
			if !slices.Contains(who, e.Who) {
				who = append(who, e.Who)
			}
		}
		return teamRow(tc, strings.Join(who, ", "), stale, now)
	})
	if len(items) == 0 {
		items = append(items, jiraPickerItem{label: "nothing in progress, nothing since " + standupDay(since, now)})
		text.WriteString("nothing since " + standupDay(since, now))
	}
	return items, strings.TrimSpace(text.String())
}

// teamRow is a card's line: title, who (or its column), how long in
// progress (stale past stale days), flag, pull request and deploy, and what
// happened since, or no activity.
func teamRow(tc teamCard, who string, stale int, now time.Time) string {
	c := tc.card
	parts := []string{cmp.Or(strings.TrimSpace(c.Key+" "+c.Summary), noTicket)}
	if who != "" {
		parts = append(parts, who)
	}
	if c.InProgress && !c.Since.IsZero() {
		days := int(now.Sub(c.Since).Hours() / 24)
		age := fmt.Sprintf("%dd", days)
		if stale > 0 && days > stale {
			age += " stale"
		}
		parts = append(parts, age)
	}
	if c.Flagged {
		parts = append(parts, "flagged")
	}
	if c.PR != "" {
		parts = append(parts, "PR "+c.PR)
	}
	if c.Deploy != "" {
		parts = append(parts, "on "+c.Deploy)
	}
	if len(tc.events) > 0 {
		parts = append(parts, standupWhat(tc.events))
	} else {
		parts = append(parts, "no activity")
	}
	return strings.Join(parts, " · ")
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
