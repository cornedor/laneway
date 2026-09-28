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

// The team's standup (tab in U) walks the board right to left, as a team
// runs its daily scrum: closest to done first, the work rather than the
// people. Each card says who has it, how long it has been in progress, a
// flag, its pull request or deploy, and what happened on it since the day
// the standup starts, or "no activity" — the stuck card worth raising. p
// groups it by person instead, for teams that go round.

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

// loadTeamStandup fetches the board's people's activity since since and
// walks the board with it, or groups it by person.
func (m *Model) loadTeamStandup(seq int, since time.Time, byPerson bool) tea.Cmd {
	now := time.Now()
	people := m.teamPeople()
	c, ctx := m.jiraClient, m.ctx
	ids := make([]string, len(people))
	for i, p := range people {
		ids[i] = p.id
	}
	cols, head, stale := m.teamColumns(), m.teamHeader(now), m.opts.staleDays
	projects := teamProjects(cols, m.jiraTab.project)
	return func() tea.Msg {
		entries, err := c.TeamStandup(ctx, since, ids)
		if err != nil {
			return standupMsg{seq: seq, err: err}
		}
		lines, folded, text := teamWalk(cols, entries, projects, byPerson, stale, since, now)
		if head != "" {
			text = head + "\n\n" + text
		}
		return standupMsg{seq: seq, lines: lines, folded: folded, head: head, text: text}
	}
}

// teamCard is a card of the walk and what happened on it.
type teamCard struct {
	card   jira.Card
	column string
	events []jira.InboxEntry
}

// teamProjects are the projects of the board's cards, and project.
func teamProjects(cols []teamColumn, project string) []string {
	var out []string
	if project != "" {
		out = append(out, project)
	}
	for _, col := range cols {
		for _, c := range col.cards {
			if p, _, ok := strings.Cut(c.Key, "-"); ok && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// teamDid is whether events hold something someone did: a comment, logged
// work, a commit, a move. Field edits alone are left to bulk changes.
func teamDid(events []jira.InboxEntry) bool {
	return slices.ContainsFunc(events, func(e jira.InboxEntry) bool {
		if e.Logged > 0 || len(e.Changes) == 0 {
			return true // a worklog, comment, commit or other note
		}
		return slices.ContainsFunc(e.Changes, func(ch jira.Change) bool { return ch.Field == "status" && ch.From != ch.To })
	})
}

// teamWalk is the Team standup's rows, the Off the board rows kept folded
// behind the one row that shows them, and the text: the columns right to
// left (or byPerson, each person's cards in that order), then what was done
// on the board's projects' other issues. A done or not started card shows
// only when something happened on it, or it is flagged; one in progress
// always does.
func teamWalk(cols []teamColumn, entries []jira.InboxEntry, projects []string, byPerson bool, stale int, since, now time.Time) (items, folded []standupLine, _ string) {
	byKey := map[string][]jira.InboxEntry{}
	for _, e := range entries {
		byKey[e.Key] = append(byKey[e.Key], e)
	}
	var walk []teamCard
	onBoard := map[string]bool{}
	for i := len(cols) - 1; i >= 0; i-- {
		for _, c := range cols[i].cards {
			onBoard[c.Key] = true
			if ev := byKey[c.Key]; standupWhat(ev) != "" || c.InProgress || c.Flagged {
				walk = append(walk, teamCard{card: c, column: cols[i].name, events: ev})
			}
		}
	}
	var text strings.Builder
	section := func(name string, list []teamCard, row func(teamCard) standupLine) {
		if len(list) == 0 {
			return
		}
		items = append(items, standupLine{head: fmt.Sprintf("%s (%d)", name, len(list))})
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(name + "\n")
		for _, tc := range list {
			line := row(tc)
			items = append(items, line)
			text.WriteString("- " + line.text() + "\n")
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
			section(n+teamLogged(events), theirs, func(tc teamCard) standupLine { return teamRow(tc, tc.column, stale, now) })
		}
	} else {
		for i := len(cols) - 1; i >= 0; i-- {
			var in []teamCard
			for _, tc := range walk {
				if tc.column == cols[i].name {
					in = append(in, tc)
				}
			}
			section(cols[i].name, in, func(tc teamCard) standupLine {
				return teamRow(tc, cmp.Or(tc.card.Assignee, "unassigned"), stale, now)
			})
		}
	}
	// What was done on the projects' issues the board doesn't show, by
	// issue, folded: it is for after the walk.
	var off []teamCard
	for _, e := range entries {
		p, _, _ := strings.Cut(e.Key, "-")
		if e.Key == "" || onBoard[e.Key] || !slices.Contains(projects, p) || !teamDid(byKey[e.Key]) ||
			slices.ContainsFunc(off, func(tc teamCard) bool { return tc.card.Key == e.Key }) {
			continue
		}
		off = append(off, teamCard{card: jira.Card{Key: e.Key, Summary: e.Summary}, events: byKey[e.Key]})
	}
	if len(items) == 0 {
		text.WriteString("nothing since " + standupDay(since, now))
	}
	if len(off) > 0 {
		items = append(items, standupLine{head: fmt.Sprintf("Off the board (%d)", len(off)), unfold: true})
		for _, tc := range off {
			var who []string
			for _, e := range tc.events {
				if !slices.Contains(who, e.Who) {
					who = append(who, e.Who)
				}
			}
			folded = append(folded, teamRow(tc, strings.Join(who, ", "), stale, now))
		}
	}
	return items, folded, strings.TrimSpace(text.String())
}

// teamRow is a card's line: title, who (or its column), how long in
// progress (stale past stale days), flag, pull request and deploy, and what
// happened since, or no activity.
func teamRow(tc teamCard, who string, stale int, now time.Time) standupLine {
	c := tc.card
	l := standupLine{key: c.Key, title: cmp.Or(strings.TrimSpace(c.Key+" "+c.Summary), noTicket), who: who,
		what: cmp.Or(standupWhat(tc.events), "no activity")}
	if c.InProgress && !c.Since.IsZero() {
		days := int(now.Sub(c.Since).Hours() / 24)
		l.age = fmt.Sprintf("%dd", days)
		if stale > 0 && days > stale {
			l.age += " stale"
		}
	}
	var marks []string
	if c.Flagged {
		marks = append(marks, "flagged")
	}
	if c.PR != "" {
		marks = append(marks, "PR "+c.PR)
	}
	if c.Deploy != "" {
		marks = append(marks, "on "+c.Deploy)
	}
	l.marks = strings.Join(marks, " · ")
	return l
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
