package jira

import (
	"cmp"
	"context"
	"net/http"
	"net/url"

	"github.com/cornedor/laneway/internal/safeterm"
)

// Link is an issue related to another: its parent, a subtask, or an issue
// link. Rel says how, from the issue's side ("parent", "subtask", "blocks",
// "is blocked by", …).
type Link struct {
	Rel     string
	Key     string
	Summary string
	Status  string
	LinkID  string // an issue link's id, for DeleteLink; "" for parent and subtasks
}

// WebLink is a remote link: a page outside Jira the issue points at.
type WebLink struct {
	Title string
	URL   string
	App   string // what made it: "Confluence", "" for a plain web link
}

// WebLinks lists key's remote links: Confluence pages, specs, incidents.
func (c *Client) WebLinks(ctx context.Context, key string) ([]WebLink, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp []struct {
		Object struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"object"`
		Application struct {
			Name string `json:"name"`
		} `json:"application"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/remotelink", key, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]WebLink, 0, len(resp))
	for _, r := range resp {
		if r.Object.URL != "" {
			out = append(out, WebLink{Title: cmp.Or(r.Object.Title, r.Object.URL), URL: r.Object.URL, App: r.Application.Name})
		}
	}
	return out, nil
}

// AddWebLink links key to the page at u, titled title (u when empty).
func (c *Client) AddWebLink(ctx context.Context, key, u, title string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{"object": map[string]string{"url": u, "title": cmp.Or(title, u)}}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/remotelink", key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

type apiLinked struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  *named `json:"status"`
	} `json:"fields"`
}

type apiIssueLink struct {
	ID   string `json:"id"`
	Type struct {
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *apiLinked `json:"inwardIssue"`
	OutwardIssue *apiLinked `json:"outwardIssue"`
}

func (a *apiLinked) link(rel string) Link {
	l := Link{Rel: safeterm.Line(rel), Key: a.Key, Summary: safeterm.Line(a.Fields.Summary)}
	if a.Fields.Status != nil {
		l.Status = safeterm.Line(a.Fields.Status.Name)
	}
	return l
}

// issueLinks flattens parent, links and subtasks, in that order.
func issueLinks(parent *apiLinked, links []apiIssueLink, subtasks []apiLinked) []Link {
	var out []Link
	if parent != nil && parent.Key != "" {
		out = append(out, parent.link("parent"))
	}
	for _, l := range links {
		switch {
		case l.OutwardIssue != nil:
			lk := l.OutwardIssue.link(l.Type.Outward)
			lk.LinkID = l.ID
			out = append(out, lk)
		case l.InwardIssue != nil:
			lk := l.InwardIssue.link(l.Type.Inward)
			lk.LinkID = l.ID
			out = append(out, lk)
		}
	}
	for i := range subtasks {
		out = append(out, subtasks[i].link("subtask"))
	}
	return out
}
