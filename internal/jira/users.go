package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// People keeps the people seen per project (internal/index), so a search
// for one needn't ask Jira each time.
type People interface {
	PutUsers(project string, us []User, assignable bool)
	SyncAssignable(project string, us []User)
	UsersSynced(project string) time.Time
	Users(project, query string, assignable bool) []User
}

const (
	// peopleFresh is how long a project's full list of assignable people
	// answers searches before it is read again; people seen meanwhile keep
	// it current.
	peopleFresh = 7 * 24 * time.Hour
	peopleShown = 50 // as many as Jira's search returns
)

// usersFrom is where AssignableUsers finds key's people: its project,
// synced from Jira when stale (once a session); nil without a cache.
func (c *Client) usersFrom(ctx context.Context, key string) (People, string) {
	if c.people == nil {
		return nil, ""
	}
	project, _, _ := strings.Cut(key, "-")
	if time.Since(c.people.UsersSynced(project)) > peopleFresh {
		c.mu.Lock()
		tried := c.peopleTried[project]
		if c.peopleTried == nil {
			c.peopleTried = map[string]bool{}
		}
		c.peopleTried[project] = true
		c.mu.Unlock()
		if !tried {
			if all, err := c.allAssignable(ctx, project); err == nil {
				c.people.SyncAssignable(project, all)
			}
		}
	}
	return c.people, project
}

// allAssignable is every person who can be assigned project's issues.
func (c *Client) allAssignable(ctx context.Context, project string) ([]User, error) {
	var out []User
	for {
		var page []struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
			Active      *bool  `json:"active"`
		}
		path := "/rest/api/3/user/assignable/search?project=" + url.QueryEscape(project) + "&maxResults=1000&startAt=" + strconv.Itoa(len(out))
		if err := c.do(ctx, http.MethodGet, path, project, nil, &page); err != nil {
			return nil, err
		}
		for _, u := range page {
			if u.Active == nil || *u.Active {
				out = append(out, User{AccountID: u.AccountID, DisplayName: u.DisplayName})
			}
		}
		if len(page) < 1000 {
			return out, nil
		}
	}
}
