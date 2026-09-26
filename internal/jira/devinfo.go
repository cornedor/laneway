package jira

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// Development info: the pull requests and branches source control links to
// an issue, from the dev-status API Jira's own development panel reads. It
// is not in the public docs, so everything here degrades to "none".

// DevItem is a pull request or branch linked to an issue.
type DevItem struct {
	Kind   string // "pr", "branch", "commit" or "build"
	Name   string // PR title, branch name, build name and number
	Status string // OPEN, MERGED, DECLINED; a build's state; "" for a branch
	Repo   string
	Branch string // a PR's source → destination
	URL    string
	Tool   string // the instance type: GitHub, GitLab, …
}

// DevInfo lists key's pull requests (open first), branches, commits and
// builds.
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
	for _, dataType := range []string{"pullrequest", "branch", "repository", "build"} {
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
			case d.Kind == "build":
				return 2
			case d.Kind == "branch":
				return 3
			}
			return 4
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
			Repositories []struct {
				Name    string `json:"name"`
				Commits []struct {
					DisplayID string `json:"displayId"`
					Message   string `json:"message"`
					URL       string `json:"url"`
					Author    struct {
						Name string `json:"name"`
					} `json:"author"`
				} `json:"commits"`
			} `json:"repositories"`
			Builds []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				BuildNumber any    `json:"buildNumber"`
				State       string `json:"state"`
				URL         string `json:"url"`
				References  []struct {
					Ref struct {
						Name string `json:"name"`
					} `json:"ref"`
				} `json:"references"`
			} `json:"builds"`
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
		for _, r := range d.Repositories {
			for _, cm := range r.Commits {
				msg, _, _ := strings.Cut(cm.Message, "\n")
				out = append(out, DevItem{Kind: "commit", Name: cm.DisplayID + " " + msg, Status: cm.Author.Name, Repo: r.Name, URL: cm.URL, Tool: tool})
			}
		}
		for _, b := range d.Builds {
			name := cmp.Or(b.DisplayName, b.Name)
			if b.BuildNumber != nil {
				name += fmt.Sprintf(" #%v", b.BuildNumber)
			}
			var ref string
			if len(b.References) > 0 {
				ref = b.References[0].Ref.Name
			}
			out = append(out, DevItem{Kind: "build", Name: name, Status: strings.ToUpper(b.State), Branch: ref, URL: b.URL, Tool: tool})
		}
	}
	return out, nil
}
