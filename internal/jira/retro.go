package jira

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RetroSprint is a closed sprint as a retro looks at it.
type RetroSprint struct {
	Name       string
	Start, End time.Time
	// Committed are the issues in it at the start, Added those joining
	// after; Done what was past the done line by its end, Carried the rest.
	Committed, Added, Done, Carried []string
	// Between are those past the left of two lines but not the right one
	// by its end, with a second line set.
	Between []string
	// Points in it and done by its end.
	Points, DonePoints float64
	// Back are the issues that moved to an earlier status category during
	// it (done back to in progress, in progress back to to do).
	Back []string
}

// Retro is the board's last n closed sprints, oldest first. done is the
// line that counts as done, nil for Jira's; compare a second line, or nil.
func (c *Client) Retro(ctx context.Context, board, n int, pointsField string, done, compare *Line) ([]RetroSprint, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	type closed struct {
		ID           int       `json:"id"`
		Name         string    `json:"name"`
		StartDate    time.Time `json:"startDate"`
		EndDate      time.Time `json:"endDate"`
		CompleteDate time.Time `json:"completeDate"`
	}
	var sprints []closed
	for start := 0; ; {
		var resp struct {
			Values []closed `json:"values"`
			IsLast bool     `json:"isLast"`
		}
		path := "/rest/agile/1.0/board/" + strconv.Itoa(board) + "/sprint?state=closed&maxResults=50&startAt=" + strconv.Itoa(start)
		if err := c.do(ctx, http.MethodGet, path, "sprints", nil, &resp); err != nil {
			return nil, err
		}
		sprints = append(sprints, resp.Values...)
		if resp.IsLast || len(resp.Values) == 0 {
			break
		}
		start += len(resp.Values)
	}
	sprints = sprints[max(len(sprints)-n, 0):]
	cats, err := c.statusCategories(ctx)
	if err != nil {
		return nil, err
	}
	rank := map[string]int{"new": 0, "indeterminate": 1, "done": 2}
	out := make([]RetroSprint, len(sprints))
	errs := make([]error, len(sprints))
	var wg sync.WaitGroup
	for i, s := range sprints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			end := s.CompleteDate
			if end.IsZero() {
				end = s.EndDate
			}
			r := RetroSprint{Name: s.Name, Start: s.StartDate, End: end}
			issues, err := c.SprintBurn(ctx, s.ID, pointsField)
			for _, is := range issues {
				r.Points += is.Points
				if !is.Added.IsZero() && is.Added.After(s.StartDate) {
					r.Added = append(r.Added, is.Key)
				} else {
					r.Committed = append(r.Committed, is.Key)
				}
				if left, right := Order(done, compare); compare != nil && left.Past(is, end) && !right.Past(is, end) {
					r.Between = append(r.Between, is.Key)
				}
				if done.Past(is, end) {
					r.Done = append(r.Done, is.Key)
					r.DonePoints += is.Points
				} else {
					r.Carried = append(r.Carried, is.Key)
				}
				for _, mv := range is.Moves {
					if mv.When.After(s.StartDate) && !mv.When.After(end) && rank[cats[mv.To]] < rank[cats[mv.From]] {
						r.Back = append(r.Back, is.Key)
						break
					}
				}
			}
			out[i], errs[i] = r, err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
