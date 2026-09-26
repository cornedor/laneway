package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What the sprint charts count, from the public API: a sprint's issues with
// their points and when they were resolved. Scope changes during a sprint
// are not tracked; an issue counts in every sprint it sits in now.

// BurnIssue is one issue of a sprint as the charts count it.
type BurnIssue struct {
	Key      string
	Points   float64
	Resolved time.Time // zero while open
	// Added is when it joined the sprint, zero when it was in it from the
	// start (or before its history); SprintBurn only.
	Added time.Time
	// Status is its status id now; Moves its status changes, oldest first
	// (SprintBurn only), to replay where it stood on a day.
	Status string
	Moves  []StatusMove
}

// StatusMove is a status change: at when, from one status id to another.
type StatusMove struct {
	When     time.Time
	From, To string
}

// StatusAt is the issue's status id at t, replayed from its moves.
func (b BurnIssue) StatusAt(t time.Time) string {
	for i := len(b.Moves) - 1; i >= 0; i-- {
		if !b.Moves[i].When.After(t) {
			return b.Moves[i].To
		}
	}
	if len(b.Moves) > 0 {
		return b.Moves[0].From
	}
	return b.Status
}

// SprintBurn lists a sprint's issues with points, resolution time and when
// they joined it. pointsField is the board's estimate field, "" for the
// detected ones.
func (c *Client) SprintBurn(ctx context.Context, sprint int, pointsField string) ([]BurnIssue, error) {
	return c.sprintIssues(ctx, sprint, pointsField, true)
}

// sprintIssues reads a sprint's issues, with when they joined it when
// changes is set (it costs each issue's changelog).
func (c *Client) sprintIssues(ctx context.Context, sprint int, pointsField string, changes bool) ([]BurnIssue, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	pf := []string{pointsField}
	if pointsField == "" {
		pf = c.resolveStoryPointFields(ctx)
	}
	expand := ""
	if changes {
		expand = "changelog"
	}
	raw, err := c.searchExpand(ctx, "sprint = "+strconv.Itoa(sprint), append([]string{"resolutiondate", "status"}, pf...), expand)
	if err != nil {
		return nil, err
	}
	out := make([]BurnIssue, len(raw))
	for i, is := range raw {
		b := BurnIssue{Key: is.Key}
		for _, id := range pf {
			if v, err := strconv.ParseFloat(string(is.Fields[id]), 64); err == nil {
				b.Points = v
				break
			}
		}
		var res string
		if json.Unmarshal(is.Fields["resolutiondate"], &res) == nil {
			b.Resolved, _ = time.Parse("2006-01-02T15:04:05.000-0700", res)
		}
		b.Added = sprintJoined(is, sprint)
		var st struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(is.Fields["status"], &st)
		b.Status = st.ID
		for _, h := range is.Changelog.Histories {
			when, err := time.Parse(jiraTime, h.Created)
			if err != nil {
				continue
			}
			for _, it := range h.Items {
				if it.Field == "status" {
					b.Moves = append(b.Moves, StatusMove{When: when, From: it.From, To: it.To})
				}
			}
		}
		slices.SortFunc(b.Moves, func(x, y StatusMove) int { return x.When.Compare(y.When) })
		out[i] = b
	}
	return out, nil
}

// SprintVelocity is one closed sprint: points in it and points done by its
// end.
type SprintVelocity struct {
	Name            string
	End             time.Time
	Committed, Done float64
}

// Velocity is the board's last n closed sprints, oldest first.
func (c *Client) Velocity(ctx context.Context, board, n int, pointsField string) ([]SprintVelocity, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	type closed struct {
		ID           int       `json:"id"`
		Name         string    `json:"name"`
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
	out := make([]SprintVelocity, len(sprints))
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
			v := SprintVelocity{Name: s.Name, End: end}
			issues, err := c.sprintIssues(ctx, s.ID, pointsField, false)
			for _, is := range issues {
				v.Committed += is.Points
				if !is.Resolved.IsZero() && !is.Resolved.After(end) {
					v.Done += is.Points
				}
			}
			out[i], errs[i] = v, err
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

// sprintJoined is the last time the issue's Sprint field gained sprint,
// zero when its history never shows that.
func sprintJoined(is rawIssue, sprint int) time.Time {
	id := strconv.Itoa(sprint)
	has := func(list string) bool {
		for _, f := range strings.Split(list, ",") {
			if strings.TrimSpace(f) == id {
				return true
			}
		}
		return false
	}
	var at time.Time
	for _, h := range is.Changelog.Histories {
		for _, it := range h.Items {
			if it.Field == "Sprint" && has(it.To) && !has(it.From) {
				if t, err := time.Parse(jiraTime, h.Created); err == nil && t.After(at) {
					at = t
				}
			}
		}
	}
	return at
}
