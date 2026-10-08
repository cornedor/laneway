package ui

import (
	"github.com/cornedor/laneway/internal/i18n"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/standup"
	"github.com/cornedor/laneway/internal/work"
)

// The standup walks the board right to left, as a team runs its daily
// scrum: closest to done first, the work rather than the people. Each card
// says who has it, how long it has been in progress, a flag, its pull
// request or deploy, and what happened on it since the day the standup
// starts, or "no activity" — the stuck card worth raising. A person's stop
// is the same walk, their cards and those they did something on.

// teamColumns are the board's columns and their cards, in board order; a
// list without lanes is one column.
func (m *Model) teamColumns() []standup.Column {
	t := m.jiraTab
	if len(t.lanes) == 0 {
		return []standup.Column{{Name: i18n.T("Issues"), Cards: slices.Clone(t.cards)}}
	}
	out := make([]standup.Column, len(t.lanes))
	for i, l := range t.lanes {
		out[i].Name = l.name
		for _, ci := range l.cards {
			out[i].Cards = append(out[i].Cards, t.cards[ci])
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
		parts = append(parts, i18n.Tf("goal: %s", g))
	}
	if !v.end.IsZero() && v.end.After(now) {
		n := workdaysLeft(now, v.end, m.opts.workdays)
		parts = append(parts, i18n.Tn(n, "%d workday left", "%d workdays left", n))
	}
	return strings.Join(parts, " · ")
}

// loadTeamStandup fetches the board's people's activity since since, with
// your commits, and makes the stops of it.
func (m *Model) loadTeamStandup(seq int, since time.Time) tea.Cmd {
	now := time.Now()
	cols := m.teamColumns()
	people := standup.People(cols)
	c, ctx, repos := m.jiraClient, m.ctx, work.Repos(m.jiraRepos)
	ids := make([]string, len(people))
	me, name := c.KnownMyself(), ""
	for i, p := range people {
		ids[i] = p.ID
		if p.ID == me {
			name = p.Name
		}
	}
	b := standup.Board{Columns: cols, Projects: teamProjects(cols, m.jiraTab.project), Stale: m.opts.staleDays, Browse: c.BrowseURL}
	head := m.teamHeader(now)
	var keys []string
	for _, col := range cols {
		for _, cd := range col.Cards {
			if !cd.Done {
				keys = append(keys, cd.Key)
			}
		}
	}
	return func() tea.Msg {
		entries, err := c.TeamStandup(ctx, since, ids)
		if err != nil {
			return standupMsg{seq: seq, err: err}
		}
		if name != "" {
			entries = standup.Mine(entries, work.Commits(repos, since), me, name)
		}
		// Blockers are a nicety: the walk goes on without them.
		b.Blockers, _ = c.Blockers(ctx, keys)
		return standupMsg{seq: seq, stops: standup.Stops(b, people, entries, since, now), head: head}
	}
}

// teamProjects are the projects of the board's cards, and project.
func teamProjects(cols []standup.Column, project string) []string {
	var out []string
	if project != "" {
		out = append(out, project)
	}
	for _, col := range cols {
		for _, c := range col.Cards {
			if p, _, ok := strings.Cut(c.Key, "-"); ok && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}
