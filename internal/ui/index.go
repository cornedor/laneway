package ui

import (
	"cmp"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// indexedIssue is key as the index last read it, for a panel read that
// couldn't reach Jira; the status line says it is the index's. Only the
// card's fields are kept: no description or comments.
func (m *Model) indexedIssue(key string, err error) (*jira.Issue, bool) {
	if err == nil || !jira.Offline(err) || m.index == nil {
		return nil, false
	}
	c, ok := m.index.Get(key)
	if !ok {
		return nil, false
	}
	m.status = "offline: " + key + " as read " + draftWhen(m.index.Synced(key), time.Now()) + ", from the index"
	iss := &jira.Issue{Key: c.Key, Summary: c.Summary, Type: c.Type, Status: c.Status, Priority: c.Priority,
		Assignee: cmp.Or(c.Assignee, "Unassigned"), Reporter: c.Reporter, Updated: c.Updated, URL: m.jiraClient.BrowseURL(c.Key),
		StoryPoints: c.Points, AssigneeAccountID: c.AssigneeID, StatusCategory: "new"}
	switch {
	case c.Done:
		iss.StatusCategory = "done"
	case c.InProgress:
		iss.StatusCategory = "indeterminate"
	}
	if c.Labels != "" {
		iss.Labels = strings.Fields(c.Labels)
	}
	return iss, true
}
