package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"
)

// Standup is your own activity since since, oldest first: status and field
// changes and comments on the issues you updated, and the work you logged.
func (c *Client) Standup(ctx context.Context, since time.Time) ([]InboxEntry, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	me, err := c.Myself(ctx)
	if err != nil {
		return nil, err
	}
	mins := int(math.Ceil(time.Since(since).Minutes())) + 1
	// updatedBy takes a user, not currentUser(): JQL won't nest functions.
	jql := fmt.Sprintf(`issue in updatedBy("%s", "-%dm") ORDER BY updated DESC`, me.AccountID, mins)
	issues, err := c.search(ctx, jql, []string{"summary"})
	if err != nil {
		return nil, err
	}
	issues = issues[:min(len(issues), c.inboxCap)]
	var (
		out  []InboxEntry
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs = make([]error, len(issues)+1)
	)
	mine := func(who string) bool { return who == me.AccountID }
	for i, is := range issues {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var summary string
			_ = json.Unmarshal(is.Fields["summary"], &summary)
			entries, err := c.issueActivity(ctx, is.Key, summary, since, mine, "")
			entries = dropWorklogChanges(entries)
			mu.Lock()
			out = append(out, entries...)
			mu.Unlock()
			errs[i] = err
		}()
	}
	// Worklogs, a day at a time back to since.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for d := since; !d.After(time.Now()); d = d.AddDate(0, 0, 1) {
			logs, err := c.MyWorklogs(ctx, d)
			if err != nil {
				errs[len(issues)] = err
				return
			}
			for _, w := range logs {
				if w.Started.Before(since) {
					continue
				}
				what := "logged " + FormatDuration(w.Seconds)
				if w.Comment != "" {
					what += ": " + w.Comment
				}
				mu.Lock()
				out = append(out, InboxEntry{Key: w.Key, Summary: w.Summary, When: w.Started, What: what})
				mu.Unlock()
			}
		}
	}()
	wg.Wait()
	if err := firstError(errs); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b InboxEntry) int { return a.When.Compare(b.When) })
	return out, nil
}

// worklogFields are the changelog's side of logging work, which the
// standup lists from the worklogs themselves.
var worklogFields = []string{"timespent", "timeestimate", "WorklogId", "WorklogTimeSpent"}

// dropWorklogChanges leaves worklogFields out of entries' changes, and an
// entry out when nothing else changed.
func dropWorklogChanges(entries []InboxEntry) []InboxEntry {
	var out []InboxEntry
	for _, e := range entries {
		if e.Changes == nil {
			out = append(out, e)
			continue
		}
		var changes []Change
		for _, ch := range e.Changes {
			if !slices.Contains(worklogFields, ch.Field) {
				changes = append(changes, ch)
			}
		}
		if len(changes) > 0 {
			e.What, e.Changes = changesText(changes), changes
			out = append(out, e)
		}
	}
	return out
}

// PreviousWorkday is the start of the last workday before now's day, the
// workdays Monday to Friday when none are given: a Monday looks back to
// Friday.
func PreviousWorkday(now time.Time, workdays []time.Weekday) time.Time {
	if len(workdays) == 0 {
		workdays = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	}
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	for !slices.Contains(workdays, d.Weekday()) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}
