package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Linking, watching and cloning issues.

// LinkType is a kind of issue link: "Blocks" reads "blocks" outward and
// "is blocked by" inward.
type LinkType struct {
	Name, Inward, Outward string
}

// LinkTypes lists the instance's link types.
func (c *Client) LinkTypes(ctx context.Context) ([]LinkType, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp struct {
		IssueLinkTypes []struct {
			Name    string `json:"name"`
			Inward  string `json:"inward"`
			Outward string `json:"outward"`
		} `json:"issueLinkTypes"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issueLinkType", "link types", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]LinkType, len(resp.IssueLinkTypes))
	for i, t := range resp.IssueLinkTypes {
		out[i] = LinkType{Name: t.Name, Inward: t.Inward, Outward: t.Outward}
	}
	return out, nil
}

// LinkIssues links from to to with typ: from does the type's outward verb
// ("A blocks B" is from A, to B). The API names the ends the other way
// round: the issue that blocks goes in inwardIssue.
func (c *Client) LinkIssues(ctx context.Context, typ, from, to string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{
		"type":         map[string]string{"name": typ},
		"inwardIssue":  map[string]string{"key": from},
		"outwardIssue": map[string]string{"key": to},
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issueLink", from, body, nil); err != nil {
		return err
	}
	c.Invalidate(from)
	c.Invalidate(to)
	return nil
}

// DeleteLink removes issue link id; key is the issue shown, to refetch.
func (c *Client) DeleteLink(ctx context.Context, key, id string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	if err := c.do(ctx, http.MethodDelete, "/rest/api/3/issueLink/"+url.PathEscape(id), key, nil, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// ToggleVote adds or takes back your vote on key and reports whether you
// now vote for it.
func (c *Client) ToggleVote(ctx context.Context, key string) (bool, error) {
	if !c.Enabled() {
		return false, errNotConfigured
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/votes"
	var resp struct {
		HasVoted bool `json:"hasVoted"`
	}
	if err := c.do(ctx, http.MethodGet, path, key, nil, &resp); err != nil {
		return false, err
	}
	method := http.MethodPost
	if resp.HasVoted {
		method = http.MethodDelete
	}
	if err := c.do(ctx, method, path, key, nil, nil); err != nil {
		return resp.HasVoted, err
	}
	return !resp.HasVoted, nil
}

// ToggleWatch starts or stops you watching key and reports whether you now
// do.
func (c *Client) ToggleWatch(ctx context.Context, key string) (bool, error) {
	if !c.Enabled() {
		return false, errNotConfigured
	}
	me, err := c.Myself(ctx)
	if err != nil {
		return false, err
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/watchers"
	var resp struct {
		IsWatching bool `json:"isWatching"`
	}
	if err := c.do(ctx, http.MethodGet, path, key, nil, &resp); err != nil {
		return false, err
	}
	if resp.IsWatching {
		err = c.do(ctx, http.MethodDelete, path+"?accountId="+url.QueryEscape(me.AccountID), key, nil, nil)
	} else {
		err = c.do(ctx, http.MethodPost, path, key, me.AccountID, nil)
	}
	return !resp.IsWatching, err
}

// Clone copies key into a new issue (CloneDraft), linked to it as a clone
// (LinkClone). It returns the new key.
func (c *Client) Clone(ctx context.Context, key string) (string, error) {
	in, err := c.CloneDraft(ctx, key)
	if err != nil {
		return "", err
	}
	nk, err := c.CreateIssue(ctx, in)
	if err != nil {
		return "", err
	}
	return nk, c.LinkClone(ctx, nk, key)
}

// CloneDraft is key's copy to create: its type, summary ("CLONE - …"),
// description, labels, priority, parent, components and fix versions.
func (c *Client) CloneDraft(ctx context.Context, key string) (NewIssue, error) {
	if !c.Enabled() {
		return NewIssue{}, errNotConfigured
	}
	var resp struct {
		Fields struct {
			IssueType   named           `json:"issuetype"`
			Summary     string          `json:"summary"`
			Description json.RawMessage `json:"description"`
			Labels      []string        `json:"labels"`
			Priority    *named          `json:"priority"`
			Parent      *struct {
				Key string `json:"key"`
			} `json:"parent"`
			Components  []named `json:"components"`
			FixVersions []named `json:"fixVersions"`
		} `json:"fields"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=issuetype,summary,description,labels,priority,parent,components,fixVersions"
	if err := c.do(ctx, http.MethodGet, path, key, nil, &resp); err != nil {
		return NewIssue{}, err
	}
	f := resp.Fields
	in := NewIssue{Project: projectOf(key), Type: f.IssueType.Name, Summary: "CLONE - " + f.Summary,
		DescriptionADF: f.Description, Labels: f.Labels}
	if f.Priority != nil {
		in.Priority = f.Priority.ID
	}
	if f.Parent != nil {
		in.Parent = f.Parent.Key
	}
	ids := func(ns []named) []map[string]string {
		out := make([]map[string]string, len(ns))
		for i, n := range ns {
			out[i] = map[string]string{"id": n.ID}
		}
		return out
	}
	in.Fields = map[string]any{}
	if len(f.Components) > 0 {
		in.Fields["components"] = ids(f.Components) // a project may require one
	}
	if len(f.FixVersions) > 0 {
		in.Fields["fixVersions"] = ids(f.FixVersions)
	}
	return in, nil
}

// LinkClone links clone to key as its clone, when the instance has that
// link type.
func (c *Client) LinkClone(ctx context.Context, clone, key string) error {
	types, _ := c.LinkTypes(ctx)
	for _, t := range types {
		if t.Name == "Cloners" {
			if err := c.LinkIssues(ctx, t.Name, clone, key); err != nil {
				return fmt.Errorf("cloned, not linked: %w", err)
			}
		}
	}
	return nil
}

// projectOf is the project part of an issue key.
func projectOf(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '-' {
			return key[:i]
		}
	}
	return key
}
