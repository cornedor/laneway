package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// A project's versions (Jira's releases): what each holds and how much of
// it is done, and releasing one.

// Version is one of a project's versions with its issues' progress.
type Version struct {
	ID, Name    string
	Released    bool
	Archived    bool
	ReleaseDate string // "2006-01-02", "" when unset
	Done, Total int
}

// Versions lists project's versions, newest first.
func (c *Client) Versions(ctx context.Context, project string) ([]Version, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var out []Version
	for start := 0; start < 1000; {
		var resp struct {
			Values []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Released    bool   `json:"released"`
				Archived    bool   `json:"archived"`
				ReleaseDate string `json:"releaseDate"`
				Status      struct {
					Unmapped   int `json:"unmapped"`
					ToDo       int `json:"toDo"`
					InProgress int `json:"inProgress"`
					Done       int `json:"done"`
				} `json:"issuesStatusForFixVersion"`
			} `json:"values"`
			IsLast bool `json:"isLast"`
		}
		q := url.Values{"expand": {"issuesstatus"}, "orderBy": {"-sequence"}, "maxResults": {"50"}, "startAt": {strconv.Itoa(start)}}
		path := "/rest/api/3/project/" + url.PathEscape(project) + "/version?" + q.Encode()
		if err := c.do(ctx, http.MethodGet, path, "versions", nil, &resp); err != nil {
			return nil, err
		}
		for _, v := range resp.Values {
			s := v.Status
			out = append(out, Version{ID: v.ID, Name: v.Name, Released: v.Released, Archived: v.Archived, ReleaseDate: v.ReleaseDate,
				Done: s.Done, Total: s.Unmapped + s.ToDo + s.InProgress + s.Done})
		}
		if resp.IsLast || len(resp.Values) < 50 { // a short page is the last
			break
		}
		start += len(resp.Values)
	}
	return out, nil
}

// ReleaseVersion marks a version released on day.
func (c *Client) ReleaseVersion(ctx context.Context, id string, day time.Time) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{"released": true, "releaseDate": day.Format(time.DateOnly)}
	return c.do(ctx, http.MethodPut, "/rest/api/3/version/"+url.PathEscape(id), "version", body, nil)
}
