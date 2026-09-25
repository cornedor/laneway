package jira

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"sort"
)

// Development info: the pull requests and branches source control links to
// an issue, from the dev-status API Jira's own development panel reads. It
// is not in the public docs, so everything here degrades to "none".

// DevItem is a pull request or branch linked to an issue.
type DevItem struct {
	Kind   string // "pr" or "branch"
	Name   string // PR title or branch name
	Status string // OPEN, MERGED, DECLINED; "" for a branch
	Repo   string
	Branch string // a PR's source → destination
	URL    string
	Tool   string // the instance type: GitHub, GitLab, …
}

// DevInfo lists key's pull requests (open first) and branches.
func (c *Client) DevInfo(ctx context.Context, key string) ([]DevItem, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var iss struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=summary", key, nil, &iss); err != nil {
		return nil, err
	}
	var sum struct {
		Summary map[string]struct {
			ByInstanceType map[string]struct {
				Count int `json:"count"`
			} `json:"byInstanceType"`
		} `json:"summary"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/dev-status/latest/issue/summary?issueId="+url.QueryEscape(iss.ID), key, nil, &sum); err != nil {
		return nil, err
	}
	var out []DevItem
	for _, dataType := range []string{"pullrequest", "branch"} {
		tools := sum.Summary[dataType].ByInstanceType
		names := make([]string, 0, len(tools))
		for t, v := range tools {
			if v.Count > 0 {
				names = append(names, t)
			}
		}
		sort.Strings(names)
		for _, tool := range names {
			items, err := c.devDetail(ctx, iss.ID, tool, dataType)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
	}
	slices.SortStableFunc(out, func(a, b DevItem) int {
		rank := func(d DevItem) int {
			switch {
			case d.Kind == "pr" && d.Status == "OPEN":
				return 0
			case d.Kind == "pr":
				return 1
			}
			return 2
		}
		return rank(a) - rank(b)
	})
	return out, nil
}

// devDetail reads one tool's pull requests or branches.
func (c *Client) devDetail(ctx context.Context, issueID, tool, dataType string) ([]DevItem, error) {
	var resp struct {
		Detail []struct {
			PullRequests []struct {
				Name   string `json:"name"`
				URL    string `json:"url"`
				Status string `json:"status"`
				Source struct {
					Branch string `json:"branch"`
				} `json:"source"`
				Destination struct {
					Branch string `json:"branch"`
				} `json:"destination"`
				RepositoryName string `json:"repositoryName"`
			} `json:"pullRequests"`
			Branches []struct {
				Name       string `json:"name"`
				URL        string `json:"url"`
				Repository struct {
					Name string `json:"name"`
				} `json:"repository"`
			} `json:"branches"`
		} `json:"detail"`
	}
	q := url.Values{"issueId": {issueID}, "applicationType": {tool}, "dataType": {dataType}}
	if err := c.do(ctx, http.MethodGet, "/rest/dev-status/latest/issue/detail?"+q.Encode(), "development info", nil, &resp); err != nil {
		return nil, err
	}
	var out []DevItem
	for _, d := range resp.Detail {
		for _, p := range d.PullRequests {
			out = append(out, DevItem{Kind: "pr", Name: p.Name, Status: p.Status, Repo: p.RepositoryName,
				Branch: p.Source.Branch + " → " + p.Destination.Branch, URL: p.URL, Tool: tool})
		}
		for _, b := range d.Branches {
			out = append(out, DevItem{Kind: "branch", Name: b.Name, Repo: b.Repository.Name, URL: b.URL, Tool: tool})
		}
	}
	return out, nil
}
