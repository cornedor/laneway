// Package standup is the daily standup the terminal and the browser show:
// the board walked right to left, closest to done first, with what
// happened on each card since a day. It comes as stops, everyone first and
// then each person, for a facilitator to cycle through.
package standup

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// Row is a row of the standup: a section's heading (Head), or an issue and
// its cells. Unfold marks the heading of the rows kept folded.
type Row struct {
	Head                              string
	Unfold                            bool
	Key, Title, Who, Age, Marks, What string
}

// Text is the row as copied: its cells joined.
func (r Row) Text() string {
	var parts []string
	for _, s := range []string{r.Title, r.Who, r.Age, r.Marks, r.What} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// Column is a board column and its cards.
type Column struct {
	Name  string
	Cards []jira.Card
}

// Person is one of the board's people; the zero Person is everyone.
type Person struct{ ID, Name, Avatar string }

// Stop is one stop of the cycle: everyone, or one person and the cards
// they have or did something on. Quiet is no activity of theirs since.
type Stop struct {
	Person       Person
	Rows, Folded []Row
	Text         string
	Quiet        bool
}

// People are the assignees of the columns' cards in the order the walk
// meets them: right to left, each column by rank.
func People(cols []Column) []Person {
	var out []Person
	for i := len(cols) - 1; i >= 0; i-- {
		for _, c := range cols[i].Cards {
			if c.AssigneeID != "" && !slices.ContainsFunc(out, func(p Person) bool { return p.ID == c.AssigneeID }) {
				out = append(out, Person{c.AssigneeID, c.Assignee, c.AvatarURL})
			}
		}
	}
	return out
}

// Board is what a walk needs beside the activity: the columns, the
// projects whose other issues show Off the board, the days in progress
// past which a card is stale, and the open issues blocking each card.
type Board struct {
	Columns  []Column
	Projects []string
	Stale    int
	Blockers map[string][]string
}

// Stops are everyone's stop, then one per person in people's order.
func Stops(b Board, people []Person, entries []jira.InboxEntry, since, now time.Time) []Stop {
	out := []Stop{walk(b, entries, Person{}, since, now)}
	for _, p := range people {
		out = append(out, walk(b, entries, p, since, now))
	}
	return out
}

// card is a card of the walk and what happened on it.
type card struct {
	card     jira.Card
	column   string
	events   []jira.InboxEntry
	blockers []string
}

// walk is p's stop (everyone's for the zero Person): the columns right to
// left, then, folded, what was done on the projects' issues the board
// doesn't show. A done or not started card shows only when something
// happened on it, or it is flagged or blocked; one in progress always does.
// A person's are those assigned to them or that they did something on.
func walk(b Board, entries []jira.InboxEntry, p Person, since, now time.Time) Stop {
	byKey := map[string][]jira.InboxEntry{}
	for _, e := range entries {
		byKey[e.Key] = append(byKey[e.Key], e)
	}
	theirs := func(events []jira.InboxEntry) bool {
		return p.ID == "" || slices.ContainsFunc(events, func(e jira.InboxEntry) bool { return e.WhoID == p.ID })
	}
	st := Stop{Person: p, Rows: []Row{}, Folded: []Row{}, Quiet: !theirs(entries) || len(entries) == 0}
	var cards []card
	onBoard := map[string]bool{}
	for i := len(b.Columns) - 1; i >= 0; i-- {
		col := b.Columns[i]
		for _, c := range col.Cards {
			onBoard[c.Key] = true
			ev := byKey[c.Key]
			if What(ev) == "" && !c.InProgress && !c.Flagged && len(b.Blockers[c.Key]) == 0 {
				continue
			}
			if p.ID != "" && c.AssigneeID != p.ID && !theirs(ev) {
				continue
			}
			cards = append(cards, card{card: c, column: col.Name, events: ev, blockers: b.Blockers[c.Key]})
		}
	}
	var text strings.Builder
	for i := len(b.Columns) - 1; i >= 0; i-- {
		var in []card
		for _, c := range cards {
			if c.column == b.Columns[i].Name {
				in = append(in, c)
			}
		}
		if len(in) == 0 {
			continue
		}
		st.Rows = append(st.Rows, Row{Head: fmt.Sprintf("%s (%d)", b.Columns[i].Name, len(in))})
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(b.Columns[i].Name + "\n")
		for _, c := range in {
			r := row(c, cmp.Or(c.card.Assignee, "unassigned"), b.Stale, now)
			st.Rows = append(st.Rows, r)
			text.WriteString("- " + r.Text() + "\n")
		}
	}
	var off []card
	for _, e := range entries {
		pr, _, _ := strings.Cut(e.Key, "-")
		if e.Key == "" || onBoard[e.Key] || !slices.Contains(b.Projects, pr) || !did(byKey[e.Key]) || !theirs(byKey[e.Key]) ||
			slices.ContainsFunc(off, func(c card) bool { return c.card.Key == e.Key }) {
			continue
		}
		off = append(off, card{card: jira.Card{Key: e.Key, Summary: e.Summary}, events: byKey[e.Key]})
	}
	if len(st.Rows) == 0 {
		text.WriteString("no changes since " + Day(since, now))
	}
	if len(off) > 0 {
		st.Rows = append(st.Rows, Row{Head: fmt.Sprintf("Off the board (%d)", len(off)), Unfold: true})
		for _, c := range off {
			var who []string
			for _, e := range c.events {
				if !slices.Contains(who, e.Who) {
					who = append(who, e.Who)
				}
			}
			st.Folded = append(st.Folded, row(c, strings.Join(who, ", "), b.Stale, now))
		}
	}
	st.Text = strings.TrimSpace(text.String())
	return st
}

// did is whether events hold something someone did: a comment, logged
// work, a commit, a move. Field edits alone are left to bulk changes.
func did(events []jira.InboxEntry) bool {
	return slices.ContainsFunc(events, func(e jira.InboxEntry) bool {
		if e.Logged > 0 || len(e.Changes) == 0 {
			return true // a worklog, comment, commit or other note
		}
		return slices.ContainsFunc(e.Changes, func(ch jira.Change) bool { return ch.Field == "status" && ch.From != ch.To })
	})
}

// row is a card's line: title, who has it, how long in progress (stale
// past stale days), flag, blockers, pull request and deploy, and what
// happened since, or no activity.
func row(c card, who string, stale int, now time.Time) Row {
	cd := c.card
	r := Row{Key: cd.Key, Title: cmp.Or(strings.TrimSpace(cd.Key+" "+cd.Summary), work.NoTicket), Who: who,
		What: cmp.Or(What(c.events), "no activity")}
	if cd.InProgress && !cd.Since.IsZero() {
		days := int(now.Sub(cd.Since).Hours() / 24)
		r.Age = fmt.Sprintf("%dd", days)
		if stale > 0 && days > stale {
			r.Age += " stale"
		}
	}
	var marks []string
	if cd.Flagged {
		marks = append(marks, "flagged")
	}
	if len(c.blockers) > 0 {
		marks = append(marks, "blocked by "+strings.Join(c.blockers, ", "))
	}
	if cd.PR != "" {
		marks = append(marks, "PR "+cd.PR)
	}
	if cd.Deploy != "" {
		marks = append(marks, "on "+cd.Deploy)
	}
	r.Marks = strings.Join(marks, " · ")
	return r
}

// What folds an issue's events into one line: the status from its first
// to its last, the time logged, and the rest counted.
func What(events []jira.InboxEntry) string {
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b jira.InboxEntry) int { return a.When.Compare(b.When) })
	var from, to string
	var fields, other []string
	logged, comments, commits := 0, 0, 0
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
				} else if !slices.Contains(fields, ch.Field) && !quietField(ch.Field) {
					fields = append(fields, ch.Field)
				}
			}
		default:
			other = append(other, e.What)
		}
	}
	var out []string
	if to != "" && to != from { // moved and back again is no move
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
	switch {
	case commits == 1:
		out = append(out, "1 commit")
	case commits > 1:
		out = append(out, strconv.Itoa(commits)+" commits")
	}
	if len(fields) > 0 {
		out = append(out, "edited "+strings.Join(fields, ", "))
	}
	return strings.Join(append(out, other...), ", ")
}

// quietField is a changelog field Jira writes beside what someone did (a
// parent set, a rank dragged) rather than an edit worth telling.
func quietField(f string) bool {
	return slices.Contains([]string{"issueparentassociation", "rank"}, strings.ToLower(f))
}

// Mine is entries with your commits, as your activity.
func Mine(entries, commits []jira.InboxEntry, me, name string) []jira.InboxEntry {
	for i := range commits {
		commits[i].WhoID, commits[i].Who = me, name
	}
	return work.WithCommits(entries, commits)
}

// Day names t's day: Today, Yesterday, else the weekday and date.
func Day(t, now time.Time) string {
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

// Since is the start of the workday lookback workdays before now's: the
// previous one for 1, so a Monday covers Friday.
func Since(now time.Time, workdays []time.Weekday, lookback int) time.Time {
	since := jira.PreviousWorkday(now, workdays)
	for range max(lookback, 1) - 1 {
		since = jira.PreviousWorkday(since, workdays)
	}
	return since
}
