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

// LinkIssues links outward to inward with typ: outward does the type's
// outward verb ("A blocks B" is outward A, inward B).
func (c *Client) LinkIssues(ctx context.Context, typ, outward, inward string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{
		"type":         map[string]string{"name": typ},
		"outwardIssue": map[string]string{"key": outward},
		"inwardIssue":  map[string]string{"key": inward},
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issueLink", outward, body, nil); err != nil {
		return err
	}
	c.Invalidate(outward)
	c.Invalidate(inward)
	return nil
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

// Clone copies key's type, summary ("CLONE - …"), description, labels,
// priority and parent into a new issue, linked to it as a clone when the
// instance has that link type. It returns the new key.
func (c *Client) Clone(ctx context.Context, key string) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
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
		} `json:"fields"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=issuetype,summary,description,labels,priority,parent"
	if err := c.do(ctx, http.MethodGet, path, key, nil, &resp); err != nil {
		return "", err
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
	nk, err := c.CreateIssue(ctx, in)
	if err != nil {
		return "", err
	}
	types, _ := c.LinkTypes(ctx)
	for _, t := range types {
		if t.Name == "Cloners" {
			if err := c.LinkIssues(ctx, t.Name, nk, key); err != nil {
				return nk, fmt.Errorf("cloned, not linked: %w", err)
			}
		}
	}
	return nk, nil
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
