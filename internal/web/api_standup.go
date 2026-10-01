package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// The standup as rows, like the TUI's (internal/ui/standup*.go): yours in
// sections, the team's walking the board right to left or by person, and
// the day view's worklog proposals.

func init() {
	get("/standup/lines", standupLines)
	get("/worklog/proposals", worklogProposals)
}

// StandupRow is a row of the standup: a section's heading (Head), or an
// issue and its cells. Unfold marks the heading of the Folded rows.
type StandupRow struct {
	Head                              string
	Unfold                            bool
	Key, Title, Who, Age, Marks, What string
}

func (l StandupRow) text() string {
	var parts []string
	for _, s := range []string{l.Title, l.Who, l.Age, l.Marks, l.What} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// standupLines: ?since=DAY&mode=mine|team|person, for the team ?board=ID and
// ?sprint=ID (0: the board's cards).
func standupLines(ctx context.Context, s *Server, r *http.Request) (any, error) {
	since, err := day(r, "since")
	if err != nil {
		return nil, err
	}
	c, now := s.Client(), time.Now()
	out := map[string]any{"Lines": []StandupRow{}, "Folded": []StandupRow{}, "Head": "", "Text": ""}
	if mode := Q(r, "mode"); mode == "team" || mode == "person" {
		board, _ := strconv.Atoi(Q(r, "board"))
		sprint, _ := strconv.Atoi(Q(r, "sprint"))
		if board == 0 {
			return nil, badRequest("need a board")
		}
		v := standupView{sprint: sprint, backlog: Q(r, "backlog") != "", jql: Q(r, "jql"), filter: Q(r, "kind") == "filter"}
		return teamStandup(ctx, s, board, v, since, now, mode == "person", out)
	}
	entries, err := c.Standup(ctx, since)
	if err != nil {
		return nil, err
	}
	entries = work.WithCommits(entries, work.Commits(work.Repos(s.opt.Jira.Repos), since))
	var keys []string
	for _, e := range entries {
		if e.Key != "" && !slices.Contains(keys, e.Key) {
			keys = append(keys, e.Key)
		}
	}
	jql := standupNext + " ORDER BY Rank"
	if len(keys) > 0 {
		jql = "key in (" + strings.Join(keys, ",") + ") OR " + standupNext + " ORDER BY Rank"
	}
	cards, err := c.SearchCards(ctx, jql)
	if err != nil && len(keys) > 0 {
		cards, _ = c.SearchCards(ctx, standupNext+" ORDER BY Rank")
	}
	lines, text := standupMine(entries, cards, c.KnownMyself(), since, now)
	out["Lines"], out["Text"] = lines, text
	return out, nil
}

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

type standupIssue struct {
	card   jira.Card
	events []jira.InboxEntry
}

func (s standupIssue) title() string {
	return cmp.Or(strings.TrimSpace(s.card.Key+" "+s.card.Summary), work.NoTicket)
}

func standupSections(entries []jira.InboxEntry, cards []jira.Card, me string) (done, doing, touched, next, blocked []standupIssue) {
	byKey := map[string]*standupIssue{}
	var order []string
	for _, e := range entries {
		k := e.Key
		if k == "" {
			k = "\x00" + e.Summary
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
		case len(next) < 3:
			next = append(next, s)
		}
		if c.Flagged {
			blocked = append(blocked, s)
		}
	}
	return done, doing, touched, next, blocked
}

func standupRow(s standupIssue, now time.Time) StandupRow {
	l := StandupRow{Key: s.card.Key, Title: s.title(), Who: s.card.Status, What: standupWhat(s.events)}
	if s.card.InProgress && !s.card.Since.IsZero() {
		l.Age = fmt.Sprintf("%dd", int(now.Sub(s.card.Since).Hours()/24))
	}
	if l.What == "" && s.card.InProgress {
		l.What = "no activity"
	}
	return l
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}

func standupQuietField(f string) bool {
	return slices.Contains([]string{"issueparentassociation", "rank"}, strings.ToLower(f))
}

func standupWhat(events []jira.InboxEntry) string {
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
				} else if !slices.Contains(fields, ch.Field) && !standupQuietField(ch.Field) {
					fields = append(fields, ch.Field)
				}
			}
		default:
			other = append(other, e.What)
		}
	}
	var out []string
	if to != "" && to != from {
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

func standupMine(entries []jira.InboxEntry, cards []jira.Card, me string, since, now time.Time) ([]StandupRow, string) {
	done, doing, touched, next, blocked := standupSections(entries, cards, me)
	items := []StandupRow{}
	section := func(name string, list []standupIssue) {
		if len(list) == 0 {
			return
		}
		items = append(items, StandupRow{Head: name})
		for _, s := range list {
			items = append(items, standupRow(s, now))
		}
	}
	section("Done since "+standupDay(since, now), done)
	section("In progress", doing)
	section("Also touched", touched)
	section("Next", next)
	section("Blockers", blocked)

	var b strings.Builder
	lines := func(head string, list []standupIssue, none string, withWhat bool) {
		b.WriteString(head + "\n")
		if len(list) == 0 && none != "" {
			b.WriteString("- " + none + "\n")
		}
		for _, s := range list {
			line := s.title()
			if withWhat && len(s.events) > 0 {
				line += ": " + standupWhat(s.events)
			}
			b.WriteString("- " + line + "\n")
		}
	}
	var yesterday, today []standupIssue
	for _, s := range slices.Concat(done, doing, touched) {
		if len(s.events) > 0 {
			yesterday = append(yesterday, s)
		}
	}
	for _, s := range slices.Concat(doing, next) {
		today = append(today, standupIssue{card: s.card})
	}
	lines("Yesterday", yesterday, "nothing on record", true)
	b.WriteString("\n")
	lines("Today", today, "", false)
	b.WriteString("\n")
	var bl []standupIssue
	for _, s := range blocked {
		bl = append(bl, standupIssue{card: s.card})
	}
	lines("Blockers", bl, "none", false)
	return items, strings.TrimSpace(b.String())
}

// ---- the team

type teamColumn struct {
	name  string
	cards []jira.Card
}

type teamCard struct {
	card     jira.Card
	column   string
	events   []jira.InboxEntry
	blockers []string
}

// standupView is the board view a team standup walks, as the board shows
// it: a sprint, the backlog, a query of the board (jql) or of all of Jira
// (filter), else the whole board.
type standupView struct {
	sprint          int
	backlog, filter bool
	jql             string
}

func teamStandup(ctx context.Context, s *Server, board int, v standupView, since, now time.Time, byPerson bool, out map[string]any) (any, error) {
	c := s.Client()
	cfg, err := c.BoardConfiguration(ctx, board)
	if err != nil {
		return nil, err
	}
	var cards []jira.Card
	switch {
	case v.sprint > 0:
		cards, _, err = c.SprintIssues(ctx, board, v.sprint, "", cfg.PointsField)
	case v.backlog:
		cards, _, err = c.BacklogIssues(ctx, board, "", cfg.PointsField)
	case v.jql != "" && v.filter:
		cards, err = c.SearchCards(ctx, v.jql)
	default:
		cards, _, err = c.BoardIssues(ctx, board, v.jql, cfg.PointsField)
	}
	if err != nil {
		return nil, err
	}
	cols := make([]teamColumn, len(cfg.Columns))
	for i, col := range cfg.Columns {
		cols[i].name = col.Name
		for _, cd := range cards {
			if slices.Contains(col.StatusIDs, cd.StatusID) {
				cols[i].cards = append(cols[i].cards, cd)
			}
		}
	}
	var ids, keys, projects []string
	for _, cd := range cards {
		if cd.AssigneeID != "" && !slices.Contains(ids, cd.AssigneeID) {
			ids = append(ids, cd.AssigneeID)
		}
		if !cd.Done {
			keys = append(keys, cd.Key)
		}
		if p, _, ok := strings.Cut(cd.Key, "-"); ok && !slices.Contains(projects, p) {
			projects = append(projects, p)
		}
	}
	if len(ids) == 0 {
		out["Head"] = ""
		return out, nil
	}
	entries, err := c.TeamStandup(ctx, since, ids)
	if err != nil {
		return nil, err
	}
	blockers, _ := c.Blockers(ctx, keys)
	head := ""
	if v.sprint > 0 {
		if sps, err := c.Sprints(ctx, board); err == nil {
			for _, sp := range sps {
				if sp.ID != v.sprint {
					continue
				}
				var parts []string
				if g := strings.Join(strings.Fields(sp.Goal), " "); g != "" {
					parts = append(parts, "goal: "+g)
				}
				if !sp.End.IsZero() && sp.End.After(now) {
					parts = append(parts, plural(workdaysLeft(now, sp.End, workdays(s)), "workday")+" left")
				}
				head = strings.Join(parts, " · ")
			}
		}
	}
	lines, folded, text := teamWalk(cols, entries, projects, byPerson, s.UIConfig().StaleDays, since, now, blockers)
	if head != "" {
		text = head + "\n\n" + text
	}
	out["Lines"], out["Folded"], out["Head"], out["Text"] = lines, folded, head, text
	return out, nil
}

func workdaysLeft(now, end time.Time, wds []time.Weekday) int {
	if len(wds) == 0 {
		wds = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	}
	end = end.In(now.Location())
	last := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, now.Location())
	n := 0
	for d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()); d.Before(last); d = d.AddDate(0, 0, 1) {
		if slices.Contains(wds, d.Weekday()) {
			n++
		}
	}
	return n
}

func teamDid(events []jira.InboxEntry) bool {
	return slices.ContainsFunc(events, func(e jira.InboxEntry) bool {
		if e.Logged > 0 || len(e.Changes) == 0 {
			return true
		}
		return slices.ContainsFunc(e.Changes, func(ch jira.Change) bool { return ch.Field == "status" && ch.From != ch.To })
	})
}

func teamWalk(cols []teamColumn, entries []jira.InboxEntry, projects []string, byPerson bool, stale int, since, now time.Time, blockers map[string][]string) (items, folded []StandupRow, _ string) {
	items, folded = []StandupRow{}, []StandupRow{}
	byKey := map[string][]jira.InboxEntry{}
	for _, e := range entries {
		byKey[e.Key] = append(byKey[e.Key], e)
	}
	var walk []teamCard
	onBoard := map[string]bool{}
	for i := len(cols) - 1; i >= 0; i-- {
		for _, c := range cols[i].cards {
			onBoard[c.Key] = true
			if ev := byKey[c.Key]; standupWhat(ev) != "" || c.InProgress || c.Flagged || len(blockers[c.Key]) > 0 {
				walk = append(walk, teamCard{card: c, column: cols[i].name, events: ev, blockers: blockers[c.Key]})
			}
		}
	}
	var text strings.Builder
	section := func(name string, list []teamCard, row func(teamCard) StandupRow) {
		if len(list) == 0 {
			return
		}
		items = append(items, StandupRow{Head: fmt.Sprintf("%s (%d)", name, len(list))})
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
			section(n+teamLogged(events), theirs, func(tc teamCard) StandupRow { return teamRow(tc, tc.column, stale, now) })
		}
	} else {
		for i := len(cols) - 1; i >= 0; i-- {
			var in []teamCard
			for _, tc := range walk {
				if tc.column == cols[i].name {
					in = append(in, tc)
				}
			}
			section(cols[i].name, in, func(tc teamCard) StandupRow { return teamRow(tc, cmp.Or(tc.card.Assignee, "unassigned"), stale, now) })
		}
	}
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
		items = append(items, StandupRow{Head: fmt.Sprintf("Off the board (%d)", len(off)), Unfold: true})
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

func teamRow(tc teamCard, who string, stale int, now time.Time) StandupRow {
	c := tc.card
	l := StandupRow{Key: c.Key, Title: cmp.Or(strings.TrimSpace(c.Key+" "+c.Summary), work.NoTicket), Who: who,
		What: cmp.Or(standupWhat(tc.events), "no activity")}
	if c.InProgress && !c.Since.IsZero() {
		days := int(now.Sub(c.Since).Hours() / 24)
		l.Age = fmt.Sprintf("%dd", days)
		if stale > 0 && days > stale {
			l.Age += " stale"
		}
	}
	var marks []string
	if c.Flagged {
		marks = append(marks, "flagged")
	}
	if len(tc.blockers) > 0 {
		marks = append(marks, "blocked by "+strings.Join(tc.blockers, ", "))
	}
	if c.PR != "" {
		marks = append(marks, "PR "+c.PR)
	}
	if c.Deploy != "" {
		marks = append(marks, "on "+c.Deploy)
	}
	l.Marks = strings.Join(marks, " · ")
	return l
}

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

// ---- worklog proposals

// worklogProposals: ?day=DAY → what commits, branch switches and ui.activity
// suggest logging, less what is logged; Failed names activity commands that broke.
func worklogProposals(ctx context.Context, s *Server, r *http.Request) (any, error) {
	d, err := day(r, "day")
	if err != nil {
		return nil, err
	}
	logs, err := s.Client().MyWorklogs(ctx, d)
	if err != nil {
		return nil, err
	}
	logged := map[string]int{}
	for _, w := range logs {
		logged[w.Key] += w.Seconds
	}
	events := work.Git(work.Repos(s.opt.Jira.Repos), d, d.AddDate(0, 0, 1))
	more, failed := work.Activity(s.UIConfig().Activity, d)
	if failed == nil {
		failed = []string{}
	}
	return map[string]any{"Items": work.Propose(append(events, more...), logged), "Failed": failed}, nil
}
