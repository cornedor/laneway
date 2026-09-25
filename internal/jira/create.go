package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Creating issues, for the thread-to-ticket action (internal/ui/threadticket.go).
// Only what a create needs: the projects the user may create in, a project's
// issue types, the labels a project already uses (so a draft sticks to the
// team's vocabulary instead of inventing new ones), and the create itself.

// Project is a Jira project the user can create issues in.
type Project struct {
	Key  string
	Name string
}

// NewIssue is a draft to create. Type is the issue type's name ("Bug");
// Description is plain text (see textToADF). Labels may be empty.
type NewIssue struct {
	Project     string
	Type        string
	Summary     string
	Description string
	Labels      []string
}

// Projects lists the projects the user can create issues in, up to 100.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp struct {
		Values []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"values"`
	}
	path := "/rest/api/3/project/search?action=create&maxResults=100"
	if err := c.do(ctx, http.MethodGet, path, "projects", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(resp.Values))
	for _, p := range resp.Values {
		out = append(out, Project{Key: p.Key, Name: p.Name})
	}
	return out, nil
}

// IssueTypes lists the non-subtask issue types creatable in project.
func (c *Client) IssueTypes(ctx context.Context, project string) ([]Option, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	type issueType struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Subtask bool   `json:"subtask"`
	}
	var resp struct {
		IssueTypes []issueType `json:"issueTypes"`
		Values     []issueType `json:"values"` // older createmeta shape
	}
	path := "/rest/api/3/issue/createmeta/" + url.PathEscape(project) + "/issuetypes"
	if err := c.do(ctx, http.MethodGet, path, project, nil, &resp); err != nil {
		return nil, err
	}
	var out []Option
	for _, t := range append(resp.IssueTypes, resp.Values...) {
		if !t.Subtask {
			out = append(out, Option{ID: t.ID, Name: t.Name})
		}
	}
	return out, nil
}

// RecentLabels returns the labels on the project's 50 most recent labelled
// issues, most used first.
func (c *Client) RecentLabels(ctx context.Context, project string) ([]string, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	body := map[string]any{
		"jql":        fmt.Sprintf("project = %q AND labels is not EMPTY ORDER BY created DESC", project),
		"fields":     []string{"labels"},
		"maxResults": 50,
	}
	var resp struct {
		Issues []struct {
			Fields struct {
				Labels []string `json:"labels"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", project+" labels", body, &resp); err != nil {
		return nil, err
	}
	count := map[string]int{}
	var order []string
	for _, is := range resp.Issues {
		for _, l := range is.Fields.Labels {
			if count[l] == 0 {
				order = append(order, l)
			}
			count[l]++
		}
	}
	// Stable insertion sort by count: the list is short.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && count[order[j]] > count[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	return order, nil
}

// CreateIssue creates the issue and returns its key.
func (c *Client) CreateIssue(ctx context.Context, in NewIssue) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
	}
	in.Project = strings.TrimSpace(in.Project)
	in.Summary = strings.TrimSpace(in.Summary)
	if in.Project == "" || in.Type == "" || in.Summary == "" {
		return "", fmt.Errorf("jira: project, type and summary are required")
	}
	fields := map[string]any{
		"project":   map[string]string{"key": in.Project},
		"issuetype": map[string]string{"name": in.Type},
		"summary":   in.Summary,
	}
	if strings.TrimSpace(in.Description) != "" {
		fields["description"] = textToADF(in.Description, nil)
	}
	if len(in.Labels) > 0 {
		fields["labels"] = in.Labels
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issue", in.Project, map[string]any{"fields": fields}, &resp); err != nil {
		return "", err
	}
	if resp.Key == "" {
		return "", fmt.Errorf("jira: create returned no key")
	}
	return resp.Key, nil
}
