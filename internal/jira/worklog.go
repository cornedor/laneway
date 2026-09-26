package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Time tracking: log work on an issue, and read back a day of your own.

// jiraTime is the timestamp shape worklogs take and return.
const jiraTime = "2006-01-02T15:04:05.000-0700"

// AddWorklog logs seconds of work on key, started at started, with an
// optional comment.
func (c *Client) AddWorklog(ctx context.Context, key string, seconds int, started time.Time, comment string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	if seconds < 60 {
		return fmt.Errorf("jira: log at least a minute")
	}
	body := map[string]any{"timeSpentSeconds": seconds, "started": started.Format(jiraTime)}
	if strings.TrimSpace(comment) != "" {
		body["comment"] = textToADF(comment, nil)
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/worklog"
	if err := c.do(ctx, http.MethodPost, path, key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// UpdateWorklog sets worklog id's time and comment.
func (c *Client) UpdateWorklog(ctx context.Context, key, id string, seconds int, comment string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	if seconds < 60 {
		return fmt.Errorf("jira: log at least a minute")
	}
	// The comment always goes along, so emptying it clears it.
	body := map[string]any{"timeSpentSeconds": seconds, "comment": textToADF(comment, nil)}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/worklog/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPut, path, key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// DeleteWorklog removes worklog id from key.
func (c *Client) DeleteWorklog(ctx context.Context, key, id string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/worklog/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodDelete, path, key, nil, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// Worklog is one entry of your own.
type Worklog struct {
	ID           string
	Key, Summary string
	Seconds      int
	Started      time.Time
	Comment      string
}

// MyWorklogs lists what you logged on day, in the order you started it.
func (c *Client) MyWorklogs(ctx context.Context, day time.Time) ([]Worklog, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	me, err := c.Myself(ctx)
	if err != nil {
		return nil, err
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	to := from.AddDate(0, 0, 1)
	jql := fmt.Sprintf(`worklogAuthor = currentUser() AND worklogDate = "%s"`, from.Format(time.DateOnly))
	issues, err := c.search(ctx, jql, []string{"summary"})
	if err != nil {
		return nil, err
	}
	var (
		out  []Worklog
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs = make([]error, len(issues))
	)
	for i, is := range issues {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var summary string
			_ = json.Unmarshal(is.Fields["summary"], &summary)
			var resp struct {
				Worklogs []struct {
					ID               string          `json:"id"`
					Author           user            `json:"author"`
					Started          string          `json:"started"`
					TimeSpentSeconds int             `json:"timeSpentSeconds"`
					Comment          json.RawMessage `json:"comment"`
				} `json:"worklogs"`
			}
			path := "/rest/api/3/issue/" + url.PathEscape(is.Key) + "/worklog?startedAfter=" +
				strconv.FormatInt(from.UnixMilli(), 10) + "&startedBefore=" + strconv.FormatInt(to.UnixMilli(), 10)
			if errs[i] = c.do(ctx, http.MethodGet, path, is.Key, nil, &resp); errs[i] != nil {
				return
			}
			for _, w := range resp.Worklogs {
				started, _ := time.Parse(jiraTime, w.Started)
				if w.Author.AccountID != me.AccountID || started.Before(from) || !started.Before(to) {
					continue
				}
				wl := Worklog{ID: w.ID, Key: is.Key, Summary: summary, Seconds: w.TimeSpentSeconds, Started: started}
				if len(w.Comment) > 0 && string(w.Comment) != "null" {
					wl.Comment = strings.TrimSpace(adfToMarkdown(w.Comment))
				}
				mu.Lock()
				out = append(out, wl)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(out, func(a, b Worklog) int { return a.Started.Compare(b.Started) })
	return out, nil
}

// ParseDuration reads leading work time off s: "1h 30m", "1h30m", "1.5h",
// "45m", "2d" (a day is 8h), and returns the seconds and what follows.
func ParseDuration(s string) (int, string, error) {
	fields := strings.Fields(s)
	secs, used := 0.0, 0
	for _, f := range fields {
		n, ok := durationToken(f)
		if !ok {
			break
		}
		secs += n
		used++
	}
	if used == 0 {
		return 0, s, fmt.Errorf("start with a time: 1h 30m, 1.5h, 45m")
	}
	return int(secs), strings.Join(fields[used:], " "), nil
}

// durationToken is one token's seconds: "1h30m" or "2.5h".
func durationToken(t string) (float64, bool) {
	unit := map[byte]float64{'d': 8 * 3600, 'h': 3600, 'm': 60}
	total, num := 0.0, ""
	for i := 0; i < len(t); i++ {
		ch := t[i]
		if u, ok := unit[ch|0x20]; ok && num != "" {
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, false
			}
			total += v * u
			num = ""
			continue
		}
		if (ch >= '0' && ch <= '9') || ch == '.' {
			num += string(ch)
			continue
		}
		return 0, false
	}
	return total, num == "" && total > 0
}

// FormatDuration is seconds as "2h 5m".
func FormatDuration(secs int) string {
	h, m := secs/3600, (secs%3600+30)/60
	if m == 60 {
		h, m = h+1, 0
	}
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
