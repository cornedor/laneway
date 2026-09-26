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
	issues = issues[:min(len(issues), inboxIssues)]
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

// PreviousWorkday is the start of the last weekday before now's day: a
// Monday looks back to Friday.
func PreviousWorkday(now time.Time) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}
