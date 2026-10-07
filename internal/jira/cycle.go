package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// CycleIssue is a done issue's lead time (created to done) and cycle time
// (first in progress to done; 0 when it never showed in progress). Wait is
// the part of it after the earlier of two lines, with a second one set.
type CycleIssue struct {
	Key, Summary      string
	Resolved          time.Time // when it got done
	Lead, Cycle, Wait time.Duration
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

// CycleTimes are project's issues done in the last weeks, oldest first,
// with their lead and cycle times from the changelog. done is the line
// that counts as done, nil for Jira's; with compare too, the line further
// right ends the cycle and Wait runs from the other.
func (c *Client) CycleTimes(ctx context.Context, project string, weeks int, done, compare *Line) ([]CycleIssue, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	cats, err := c.statusCategories(ctx)
	if err != nil {
		return nil, err
	}
	end, mid := done, (*Line)(nil)
	if compare != nil {
		mid, end = Order(done, compare)
	}
	jql := fmt.Sprintf(`project = "%s" AND statusCategory = Done AND resolved >= -%dw ORDER BY resolved ASC`, project, weeks)
	if end != nil {
		jql = fmt.Sprintf(`project = "%s" AND status changed after -%dw ORDER BY updated ASC`, project, weeks)
	}
	raw, err := c.searchChart(ctx, jql, []string{"summary", "created", "resolutiondate", "status"}, "changelog")
	if err != nil {
		return nil, err
	}
	from := time.Now().AddDate(0, 0, -7*weeks)
	var out []CycleIssue
	for _, is := range raw {
		var summary, created, resolved string
		var st struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(is.Fields["summary"], &summary)
		_ = json.Unmarshal(is.Fields["created"], &created)
		_ = json.Unmarshal(is.Fields["resolutiondate"], &resolved)
		_ = json.Unmarshal(is.Fields["status"], &st)
		ct, err := time.Parse(jiraTime, created)
		if err != nil {
			continue
		}
		b := BurnIssue{Key: is.Key, Status: st.ID, Moves: statusMoves(is)}
		b.Resolved, _ = time.Parse(jiraTime, resolved)
		rt, ok := end.Since(b)
		if !ok || rt.IsZero() || end != nil && rt.Before(from) {
			continue
		}
		ci := CycleIssue{Key: is.Key, Summary: summary, Resolved: rt, Lead: rt.Sub(ct)}
		var started time.Time
		for _, mv := range b.Moves {
			if cats[mv.To] == "indeterminate" {
				started = mv.When
				break
			}
		}
		if !started.IsZero() && started.Before(rt) {
			ci.Cycle = rt.Sub(started)
		}
		if mid != nil {
			if m, ok := mid.Since(b); ok && !m.IsZero() {
				if m.Before(started) {
					m = started
				}
				ci.Wait = max(rt.Sub(m), 0)
			}
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
