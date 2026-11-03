// Package jirasheet is the timesheet provider on Jira's worklogs: your own
// worklogs are the entries, and laneway keeps the timer itself.
package jirasheet

import (
	"context"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/timesheet"
)

// Sheet logs time as worklogs on c's site.
type Sheet struct{ c *jira.Client }

// New is the provider on c.
func New(c *jira.Client) *Sheet { return &Sheet{c: c} }

func (s *Sheet) Name() string { return "Jira" }

// Caps: Move is false on Jira Server and Data Center, and once the site
// turned a move away.
func (s *Sheet) Caps() timesheet.Caps {
	return timesheet.Caps{Move: s.c.MovesWorklogs(), Remaining: true}
}

func (s *Sheet) Entries(ctx context.Context, from, to time.Time) ([]timesheet.Entry, error) {
	logs, err := s.c.MyWorklogsBetween(ctx, from, to)
	out := make([]timesheet.Entry, len(logs))
	for i, w := range logs {
		out[i] = timesheet.Entry{ID: w.ID, Key: w.Key, Summary: w.Summary, Started: w.Started, Seconds: w.Seconds, Comment: w.Comment}
	}
	return out, err
}

func (s *Sheet) Add(ctx context.Context, e timesheet.New) error {
	return s.c.AddWorklog(ctx, e.Key, e.Seconds, e.Started, e.Comment, e.Left)
}

func (s *Sheet) Update(ctx context.Context, key, id string, seconds int, started time.Time, comment *string) error {
	return s.c.UpdateWorklog(ctx, key, id, seconds, started, comment)
}

func (s *Sheet) Delete(ctx context.Context, key string, ids []string) error {
	return s.c.DeleteWorklogs(ctx, key, ids)
}

func (s *Sheet) Move(ctx context.Context, key string, ids []string, to string) error {
	return s.c.MoveWorklogs(ctx, key, ids, to)
}

func (s *Sheet) Running(context.Context) (*timesheet.Entry, error) { return nil, nil }

func (s *Sheet) Start(context.Context, string, string, time.Time) error {
	return timesheet.ErrUnsupported
}
