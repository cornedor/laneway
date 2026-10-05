package web

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/calendar"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/standup"
	"github.com/cornedor/laneway/internal/work"
)

// The standup as the TUI's (internal/ui/standup*.go): the board walked
// right to left, as stops for everyone and then each person, and the day
// view's worklog proposals.

func init() {
	get("/standup/lines", standupLines)
	get("/worklog/proposals", worklogProposals)
}

// standupLines: ?board=ID and its view (?sprint=ID, ?backlog=1, ?jql= with
// ?kind=filter for all of Jira; else the board's cards), ?since=DAY or
// ui.standup_lookback workdays back. Board is the view's columns and their
// cards, for the board beside the standup. Since is the day it starts from,
// Settings the ui.standup_* options, durations in seconds; the browser
// splits Length over who takes part, as standup.Settings.Turn does.
func standupLines(ctx context.Context, s *Server, r *http.Request) (any, error) {
	now := time.Now()
	set, _ := standup.Parse(s.UIConfig())
	since := standup.Since(now, workdays(s), set.Lookback)
	if Q(r, "since") != "" {
		var err error
		if since, err = day(r, "since"); err != nil {
			return nil, err
		}
	}
	board, _ := strconv.Atoi(Q(r, "board"))
	if board == 0 {
		return nil, badRequest("need a board")
	}
	sprint, _ := strconv.Atoi(Q(r, "sprint"))
	v := standupView{sprint: sprint, backlog: Q(r, "backlog") != "", jql: Q(r, "jql"), filter: Q(r, "kind") == "filter"}
	stops, cols, head, err := teamStandup(ctx, s, board, v, since, now)
	if err != nil {
		return nil, err
	}
	for i := range cols {
		if cols[i].Cards == nil {
			cols[i].Cards = []jira.Card{}
		}
	}
	return map[string]any{"Stops": stops, "Board": cols, "Head": head, "Since": since.Format(time.DateOnly), "Settings": map[string]any{
		"First": set.First, "Shuffle": set.Shuffle, "Length": set.Length.Seconds(), "Timebox": set.Timebox.Seconds(),
	}}, nil
}

// standupView is the board view a standup walks, as the board shows it: a
// sprint, the backlog, a query of the board (jql) or of all of Jira
// (filter), else the whole board.
type standupView struct {
	sprint          int
	backlog, filter bool
	jql             string
}

// teamStandup is the stops of the board's view since since, its columns
// and cards, and its head: the sprint goal and the workdays left.
func teamStandup(ctx context.Context, s *Server, board int, v standupView, since, now time.Time) ([]standup.Stop, []standup.Column, string, error) {
	c := s.Client()
	cfg, err := c.BoardConfiguration(ctx, board)
	if err != nil {
		return nil, nil, "", err
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
		return nil, nil, "", err
	}
	b := standup.Board{Columns: make([]standup.Column, len(cfg.Columns)), Stale: cmp.Or(s.UIConfig().StaleDays, 5)}
	for i, col := range cfg.Columns {
		b.Columns[i].Name = col.Name
		for _, cd := range cards {
			if slices.Contains(col.StatusIDs, cd.StatusID) {
				b.Columns[i].Cards = append(b.Columns[i].Cards, cd)
			}
		}
	}
	people := standup.People(b.Columns)
	var keys []string
	for _, cd := range cards {
		if !cd.Done {
			keys = append(keys, cd.Key)
		}
		if p, _, ok := strings.Cut(cd.Key, "-"); ok && !slices.Contains(b.Projects, p) {
			b.Projects = append(b.Projects, p)
		}
	}
	ids := make([]string, len(people))
	me, name := c.KnownMyself(), ""
	for i, p := range people {
		ids[i] = p.ID
		if p.ID == me {
			name = p.Name
		}
	}
	entries, err := c.TeamStandup(ctx, since, ids)
	if err != nil {
		return nil, nil, "", err
	}
	if name != "" {
		entries = standup.Mine(entries, work.Commits(work.Repos(s.opt.Jira.Repos), since), me, name)
	}
	b.Blockers, _ = c.Blockers(ctx, keys)
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
	return standup.Stops(b, people, entries, since, now), b.Columns, head, nil
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
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
	ui := s.UIConfig()
	meetings, calErr := calendar.Day(ctx, ui.Calendar, strings.TrimSpace(ui.MeetingKey), d, logs)
	ps, failed := work.Day(work.Repos(s.opt.Jira.Repos), ui.Activity, d, logs, meetings)
	if failed == nil {
		failed = []string{}
	}
	out := map[string]any{"Items": ps, "Failed": failed}
	if calErr != nil {
		out["Calendar"] = calErr.Error()
	}
	return out, nil
}
