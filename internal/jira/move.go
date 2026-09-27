package jira

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// movePoll is how often MoveIssue asks after the move's progress.
var movePoll = time.Second

// MoveTypes lists the issue types in project to that an issue of type
// current in from can move to. A subtask moves with its parent, so it has
// none.
func (c *Client) MoveTypes(ctx context.Context, from, current, to string) ([]Option, error) {
	src, err := c.allIssueTypes(ctx, from)
	if err != nil {
		return nil, err
	}
	if slices.ContainsFunc(src, func(t issueType) bool { return t.Subtask && strings.EqualFold(t.Name, current) }) {
		return nil, errors.New("a subtask moves with its parent")
	}
	return c.IssueTypes(ctx, to)
}

// MoveIssue moves key to project as the issue type typeID, with its
// subtasks, keeping field values and falling back to the target's default
// status, and returns its new key.
func (c *Client) MoveIssue(ctx context.Context, key, project, typeID string) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
	}
	body := map[string]any{
		"sendBulkNotification": false,
		"targetToSourcesMapping": map[string]any{
			project + "," + typeID: map[string]any{
				"issueIdsOrKeys":              []string{key},
				"inferFieldDefaults":          true,
				"inferStatusDefaults":         true,
				"inferSubtaskTypeDefault":     true,
				"inferClassificationDefaults": true,
			},
		},
	}
	var task struct {
		TaskID string `json:"taskId"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/bulk/issues/move", key, body, &task); err != nil {
		return "", err
	}
	var p struct {
		Status    string              `json:"status"`
		Failed    map[string][]string `json:"failedAccessibleIssues"`
		Processed []int64             `json:"processedAccessibleIssues"`
	}
	for {
		if err := c.do(ctx, http.MethodGet, "/rest/api/3/bulk/queue/"+url.PathEscape(task.TaskID), "move", nil, &p); err != nil {
			return "", err
		}
		if p.Status != "ENQUEUED" && p.Status != "RUNNING" {
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("move still running: %w", ctx.Err())
		case <-time.After(movePoll):
		}
	}
	c.Invalidate(key)
	var why []string
	for _, w := range p.Failed {
		why = append(why, w...)
	}
	if len(why) > 0 {
		return "", errors.New(strings.Join(why, "; "))
	}
	switch {
	case p.Status != "COMPLETE":
		return "", fmt.Errorf("move %s", strings.ToLower(cmp.Or(p.Status, "failed")))
	case len(p.Processed) == 0:
		return "", errors.New("Jira moved nothing: the Move and Bulk change permissions?")
	}
	// The old key still finds the issue, under its new one.
	var moved struct {
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=summary", key, nil, &moved); err != nil {
		return "", err
	}
	return moved.Key, nil
}
