package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// CycleIssue is a resolved issue's lead time (created to done) and cycle
// time (first in progress to done; 0 when it never showed in progress).
type CycleIssue struct {
	Key, Summary string
	Resolved     time.Time
	Lead, Cycle  time.Duration
}

// statusCategories maps each status id to its category key: new,
// indeterminate or done.
func (c *Client) statusCategories(ctx context.Context) (map[string]string, error) {
	var resp []struct {
		ID       string `json:"id"`
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/status", "statuses", nil, &resp); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(resp))
	for _, s := range resp {
		out[s.ID] = s.Category.Key
	}
	return out, nil
}

// CycleTimes are project's issues resolved in the last weeks, oldest
// first, with their lead and cycle times from the changelog.
func (c *Client) CycleTimes(ctx context.Context, project string, weeks int) ([]CycleIssue, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	cats, err := c.statusCategories(ctx)
	if err != nil {
		return nil, err
	}
	jql := fmt.Sprintf(`project = "%s" AND statusCategory = Done AND resolved >= -%dw ORDER BY resolved ASC`, project, weeks)
	raw, err := c.searchChart(ctx, jql, []string{"summary", "created", "resolutiondate"}, "changelog")
	if err != nil {
		return nil, err
	}
	var out []CycleIssue
	for _, is := range raw {
		var summary, created, resolved string
		_ = json.Unmarshal(is.Fields["summary"], &summary)
		_ = json.Unmarshal(is.Fields["created"], &created)
		_ = json.Unmarshal(is.Fields["resolutiondate"], &resolved)
		ct, err1 := time.Parse(jiraTime, created)
		rt, err2 := time.Parse(jiraTime, resolved)
		if err1 != nil || err2 != nil {
			continue
		}
		ci := CycleIssue{Key: is.Key, Summary: summary, Resolved: rt, Lead: rt.Sub(ct)}
		var started time.Time
		for _, h := range is.Changelog.Histories {
			at, _ := time.Parse(jiraTime, h.Created)
			for _, it := range h.Items {
				if it.Field == "status" && cats[it.To] == "indeterminate" && (started.IsZero() || at.Before(started)) {
					started = at
				}
			}
		}
		if !started.IsZero() && started.Before(rt) {
			ci.Cycle = rt.Sub(started)
		}
		out = append(out, ci)
	}
	slices.SortFunc(out, func(a, b CycleIssue) int { return a.Resolved.Compare(b.Resolved) })
	return out, nil
}

// Percentile is the p-th (0–100) of ds, nearest rank; 0 for none.
func Percentile(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(ds))
	i := (p*len(s) + 99) / 100
	return s[min(max(i-1, 0), len(s)-1)]
}
