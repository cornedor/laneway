package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// Your standup (U) is one row per issue, not per event, in sections: done
// since the previous workday, in progress (with or without activity, and
// how long), next in the sprint by rank, and blockers (flagged). Copy as
// text gives Yesterday / Today / Blockers for a standup channel.

// standupNext is how many to-dos Next lists.
const standupNext = 3

// standupJQL finds the cards the sections need beside the activity: those
// you touched, and your open ones in the open sprints, by rank.
func standupJQL(keys []string) string {
	mine := "(assignee = currentUser() AND statusCategory != Done AND sprint in openSprints())"
	if len(keys) == 0 {
		return mine + " ORDER BY Rank"
	}
	return "key in (" + strings.Join(keys, ",") + ") OR " + mine + " ORDER BY Rank"
}

// standupIssue is one issue's row: its card (Key alone when not found) and
// what you did to it.
type standupIssue struct {
	card   jira.Card
	events []jira.InboxEntry
}

func (s standupIssue) title() string {
	return cmp.Or(strings.TrimSpace(s.card.Key+" "+s.card.Summary), noTicket)
}

// standupSections sorts the activity and cards into the sections, each in
// the cards' rank order; activity on issues without a card keeps its order.
func standupSections(entries []jira.InboxEntry, cards []jira.Card, me string) (done, doing, touched, next, blocked []standupIssue) {
	byKey := map[string]*standupIssue{}
	var order []string
	for _, e := range entries {
		k := e.Key
		if k == "" {
			k = "\x00" + e.Summary // commits without a ticket, by message
		}
		s, ok := byKey[k]
		if !ok {
			s = &standupIssue{card: jira.Card{Key: e.Key, Summary: e.Summary}}
			byKey[k] = s
			order = append(order, k)
		}
		s.events = append(s.events, e)
	}
	rank := map[string]int{}
	for i, c := range cards {
		rank[c.Key] = i
		if s, ok := byKey[c.Key]; ok {
			s.card = c
		}
	}
	slices.SortStableFunc(order, func(a, b string) int {
		ra, oka := rank[a]
		rb, okb := rank[b]
		switch {
		case oka && okb:
			return ra - rb
		case oka:
			return -1
		case okb:
			return 1
		}
		return 0
	})
	for _, k := range order {
		s := *byKey[k]
		switch {
		case s.card.Done:
			done = append(done, s)
		case s.card.InProgress:
			doing = append(doing, s)
		default:
			touched = append(touched, s)
		}
		if s.card.Flagged && !s.card.Done {
			blocked = append(blocked, s)
		}
	}
	for _, c := range cards {
		if _, ok := byKey[c.Key]; ok || c.Done || (me != "" && c.AssigneeID != me) {
			continue
		}
		s := standupIssue{card: c}
		switch {
		case c.InProgress:
			doing = append(doing, s)
		case len(next) < standupNext:
			next = append(next, s)
		}
		if c.Flagged {
			blocked = append(blocked, s)
		}
	}
	return done, doing, touched, next, blocked
}

// standupRow is an issue's line: title, status now, what changed or how
// long it has sat.
func standupRow(s standupIssue, now time.Time) string {
	parts := []string{s.title()}
	if st := s.card.Status; st != "" {
		parts = append(parts, st)
	}
	switch {
	case len(s.events) > 0:
		parts = append(parts, standupWhat(s.events))
	case s.card.InProgress && !s.card.Since.IsZero():
		parts = append(parts, fmt.Sprintf("no activity · in progress %dd", int(now.Sub(s.card.Since).Hours()/24)))
	case s.card.InProgress:
		parts = append(parts, "no activity")
	}
	return strings.Join(parts, " · ")
}

// standupWhat folds an issue's events into one line: the status from its
// first to its last, the time logged, and the rest counted.
func standupWhat(events []jira.InboxEntry) string {
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b jira.InboxEntry) int { return a.When.Compare(b.When) })
	var from, to string
	var fields []string
	logged, comments, commits := 0, 0, 0
	var other []string
	for _, e := range events {
		switch {
		case e.Logged > 0:
			logged += e.Logged
		case strings.HasPrefix(e.What, "commented: "), strings.HasPrefix(e.What, "mentioned you: "):
			comments++
		case strings.HasPrefix(e.What, "commit: "):
			commits++
		case len(e.Changes) > 0:
			for _, ch := range e.Changes {
				if ch.Field == "status" {
					if from == "" {
						from = ch.From
					}
					to = ch.To
				} else if !slices.Contains(fields, ch.Field) {
					fields = append(fields, ch.Field)
				}
			}
		default:
			other = append(other, e.What)
		}
	}
	var out []string
	if to != "" {
		out = append(out, cmp.Or(from, "—")+" → "+to)
	}
	if logged > 0 {
		out = append(out, "logged "+jira.FormatDuration(logged))
	}
	switch {
	case comments == 1:
		out = append(out, "commented")
	case comments > 1:
		out = append(out, fmt.Sprintf("%d comments", comments))
	}
	if commits > 0 {
		out = append(out, plural(commits, "commit"))
	}
	if len(fields) > 0 {
		out = append(out, "edited "+strings.Join(fields, ", "))
	}
	return strings.Join(append(out, other...), ", ")
}

// standupMine is the picker's rows (after the copy and step rows) and the
// Yesterday / Today / Blockers text.
func standupMine(entries []jira.InboxEntry, cards []jira.Card, me string, since, now time.Time) ([]jiraPickerItem, string) {
	done, doing, touched, next, blocked := standupSections(entries, cards, me)
	var items []jiraPickerItem
	section := func(name string, list []standupIssue) {
		if len(list) == 0 {
			return
		}
		items = append(items, jiraPickerItem{label: "── " + name})
		for _, s := range list {
			items = append(items, jiraPickerItem{id: s.card.Key, label: "  " + standupRow(s, now)})
		}
	}
	section("Done since "+standupDay(since, now), done)
	section("In progress", doing)
	section("Also touched", touched)
	section("Next", next)
	section("Blockers", blocked)

	var b strings.Builder
	lines := func(head string, list []standupIssue, none string) {
		b.WriteString(head + "\n")
		if len(list) == 0 && none != "" {
			b.WriteString("- " + none + "\n")
		}
		for _, s := range list {
			line := s.title()
			if len(s.events) > 0 {
				line += ": " + standupWhat(s.events)
			}
			b.WriteString("- " + line + "\n")
		}
	}
	var yesterday []standupIssue
	for _, s := range slices.Concat(done, doing, touched) {
		if len(s.events) > 0 {
			yesterday = append(yesterday, s)
		}
	}
	var today []standupIssue
	for _, s := range slices.Concat(doing, next) {
		today = append(today, standupIssue{card: s.card})
	}
	lines("Yesterday", yesterday, "nothing on record")
	b.WriteString("\n")
	lines("Today", today, "")
	b.WriteString("\n")
	blockers := make([]standupIssue, len(blocked))
	for i, s := range blocked {
		blockers[i] = standupIssue{card: s.card}
	}
	lines("Blockers", blockers, "none")
	return items, strings.TrimSpace(b.String())
}
