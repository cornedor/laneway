package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// StatusTime is how long an issue sat in one status, over every visit.
type StatusTime struct {
	Status string
	Time   time.Duration
	Visits int
	Now    bool // the status it is in
}

// TimeInStatus replays key's status changes since it was created: the time
// in each status, in the order first reached, up to now for the current.
func (c *Client) TimeInStatus(ctx context.Context, key string, now time.Time) ([]StatusTime, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	base := "/rest/api/3/issue/" + url.PathEscape(key)
	var is struct {
		Fields struct {
			Created string `json:"created"`
			Status  named  `json:"status"`
		} `json:"fields"`
	}
	if err := c.do(ctx, http.MethodGet, base+"?fields=created,status", key, nil, &is); err != nil {
		return nil, err
	}
	type move struct {
		at       time.Time
		from, to string
	}
	var moves []move
	for start := 0; start < 5000; {
		var log struct {
			Values []struct {
				Created string `json:"created"`
				Items   []struct {
					Field      string `json:"field"`
					FromString string `json:"fromString"`
					ToString   string `json:"toString"`
				} `json:"items"`
			} `json:"values"`
			IsLast bool `json:"isLast"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"100"}}
		if err := c.do(ctx, http.MethodGet, base+"/changelog?"+q.Encode(), "changelog", nil, &log); err != nil {
			return nil, err
		}
		for _, h := range log.Values {
			at, _ := time.Parse(jiraTime, h.Created)
			for _, it := range h.Items {
				if it.Field == "status" {
					moves = append(moves, move{at, it.FromString, it.ToString})
				}
			}
		}
		if log.IsLast || len(log.Values) < 100 {
			break
		}
		start += len(log.Values)
	}
	created, _ := time.Parse(jiraTime, is.Fields.Created)
	status := is.Fields.Status.Name
	if len(moves) > 0 {
		status = moves[0].from
	}
	var out []StatusTime
	add := func(s string, d time.Duration) {
		for i := range out {
			if out[i].Status == s {
				out[i].Time += d
				out[i].Visits++
				return
			}
		}
		out = append(out, StatusTime{Status: s, Time: d, Visits: 1})
	}
	since := created
	for _, mv := range moves {
		add(status, max(mv.at.Sub(since), 0))
		status, since = mv.to, mv.at
	}
	add(status, max(now.Sub(since), 0))
	for i := range out {
		out[i].Now = out[i].Status == status
	}
	return out, nil
}
