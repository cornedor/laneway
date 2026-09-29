package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The inbox: what others did on the issues you watch, are assigned or
// reported — field changes, comments, and comments that mention you. The
// ui keeps it per issue: InboxIssues finds them, IssueInbox reads one.

// inboxIssues caps how many recently updated issues the inbox reads, unless
// Config.InboxIssues says otherwise.
const inboxIssues = 30

// InboxEntry is one thing that happened on an issue.
type InboxEntry struct {
	Key, Summary string
	When         time.Time
	Who, WhoID   string
	What         string // "Status: To Do → Done", "commented: …"
	Mention      bool   // a comment that mentions you
	Assigned     bool   // a change that made you the assignee
	// CommentID and Body are a comment's, Body as markdown; "" for others.
	CommentID, Body string
	// Changes are a changelog entry's fields one by one, for showing a
	// long one (the description) as a diff; none for other entries.
	Changes []Change
	// Logged is a worklog entry's seconds (the standup's); 0 for others.
	Logged int
}

// Change is one field of a changelog entry, before and after.
type Change struct {
	Field, From, To string
	to              string // the new value's id: an assignee's accountId
}

// InboxIssue is an issue of yours as the inbox lists it.
type InboxIssue struct {
	Key, Summary, Status, Assignee string
	Updated                        time.Time
}

// InboxIssues are your issues updated since since, newest first, up to the
// inbox cap.
func (c *Client) InboxIssues(ctx context.Context, since time.Time) ([]InboxIssue, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	issues, err := c.search(ctx, inboxJQL(since)+" ORDER BY updated DESC", []string{"summary", "status", "assignee", "updated"})
	if err != nil {
		return nil, err
	}
	issues = issues[:min(len(issues), c.inboxCap)]
	out := make([]InboxIssue, len(issues))
	for i, is := range issues {
		var status struct {
			Name string `json:"name"`
		}
		var assignee user
		var updated string
		_ = json.Unmarshal(is.Fields["summary"], &out[i].Summary)
		_ = json.Unmarshal(is.Fields["status"], &status)
		_ = json.Unmarshal(is.Fields["assignee"], &assignee)
		_ = json.Unmarshal(is.Fields["updated"], &updated)
		out[i].Key, out[i].Status, out[i].Assignee = is.Key, status.Name, assignee.DisplayName
		out[i].Updated, _ = time.Parse(jiraTime, updated)
	}
	return out, nil
}

// IssueInbox is what others did on key since since, oldest first; Rank
// changes, which no one reads, are left out.
func (c *Client) IssueInbox(ctx context.Context, key, summary string, since time.Time) ([]InboxEntry, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	me, err := c.Myself(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := c.issueActivity(ctx, key, summary, since, func(who string) bool { return who != me.AccountID }, me.AccountID)
	if err != nil {
		return nil, err
	}
	out := entries[:0]
	for _, e := range entries {
		e.Changes = slices.DeleteFunc(e.Changes, func(ch Change) bool { return ch.Field == "Rank" })
		if e.CommentID == "" && e.Logged == 0 && len(e.Changes) == 0 {
			continue
		}
		if e.CommentID == "" {
			e.What = changesText(e.Changes)
		}
		e.Assigned = slices.ContainsFunc(e.Changes, func(ch Change) bool { return ch.Field == "assignee" && ch.to == me.AccountID })
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(a, b InboxEntry) int { return a.When.Compare(b.When) })
	return out, nil
}

// inboxJQL finds your issues updated since since. Relative minutes sidestep
// the profile time zone JQL dates are read in.
func inboxJQL(since time.Time) string {
	mins := int(math.Ceil(time.Since(since).Minutes())) + 1
	return fmt.Sprintf("(watcher = currentUser() OR assignee = currentUser() OR reporter = currentUser()) AND updated >= -%dm", mins)
}

func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// issueActivity is one issue's changes and comments since since by the
// authors keep accepts; me marks comments mentioning you.
func (c *Client) issueActivity(ctx context.Context, key, summary string, since time.Time, keep func(accountID string) bool, me string) ([]InboxEntry, error) {
	out, err := c.issueChanges(ctx, key, summary, since, keep)
	if err != nil {
		return nil, err
	}
	base := "/rest/api/3/issue/" + url.PathEscape(key)
	var comments struct {
		Comments []struct {
			ID      string          `json:"id"`
			Author  user            `json:"author"`
			Created string          `json:"created"`
			Body    json.RawMessage `json:"body"`
		} `json:"comments"`
	}
	if err := c.do(ctx, http.MethodGet, base+"/comment?orderBy=-created&maxResults=20", "comments", nil, &comments); err != nil {
		return nil, err
	}
	for _, cm := range comments.Comments {
		when, _ := time.Parse(jiraTime, cm.Created)
		if !keep(cm.Author.AccountID) || !when.After(since) {
			continue
		}
		body := adfToMarkdown(cm.Body)
		text := strings.Join(strings.Fields(body), " ")
		mention := mentions(cm.Body, me)
		what := "commented: " + text
		if mention {
			what = "mentioned you: " + text
		}
		out = append(out, InboxEntry{Key: key, Summary: summary, When: when, Who: cm.Author.DisplayName, WhoID: cm.Author.AccountID,
			What: what, Mention: mention, CommentID: cm.ID, Body: body})
	}
	return out, nil
}

// Changelog is key's field changes by anyone, oldest first: its latest
// changelogTail.
func (c *Client) Changelog(ctx context.Context, key string) ([]InboxEntry, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	return c.issueChanges(ctx, key, "", time.Time{}, func(string) bool { return true })
}

// issueChanges is key's latest changelogTail changes after since by the
// authors keep accepts, oldest first.
func (c *Client) issueChanges(ctx context.Context, key, summary string, since time.Time, keep func(accountID string) bool) ([]InboxEntry, error) {
	base := "/rest/api/3/issue/" + url.PathEscape(key)
	var log struct {
		Total  int `json:"total"`
		Values []struct {
			Author  user   `json:"author"`
			Created string `json:"created"`
			Items   []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
				To         string `json:"to"`
			} `json:"items"`
		} `json:"values"`
	}
	getLog := func(start int) error {
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {strconv.Itoa(changelogTail)}}
		return c.do(ctx, http.MethodGet, base+"/changelog?"+q.Encode(), "changelog", nil, &log)
	}
	// Oldest first: jump to the tail once the total is known.
	if err := getLog(0); err != nil {
		return nil, err
	}
	if log.Total > changelogTail {
		if err := getLog(log.Total - changelogTail); err != nil {
			return nil, err
		}
	}
	var out []InboxEntry
	for _, h := range log.Values {
		when, _ := time.Parse(jiraTime, h.Created)
		if !keep(h.Author.AccountID) || !when.After(since) {
			continue
		}
		var changes []Change
		for _, it := range h.Items {
			changes = append(changes, Change{Field: it.Field, From: it.FromString, To: it.ToString, to: it.To})
		}
		changes = foldWorklog(changes)
		if len(changes) > 0 {
			out = append(out, InboxEntry{Key: key, Summary: summary, When: when, Who: h.Author.DisplayName, WhoID: h.Author.AccountID,
				What: changesText(changes), Changes: changes})
		}
	}
	return out, nil
}

// LoggedField is the Change foldWorklog makes of logging work: To is the
// time logged ("1h"), or removed ("-1h").
const LoggedField = "logged"

// foldWorklog turns the items logging work writes to the changelog
// (timespent 3600 → 7200, WorklogId — → 10042, the remaining estimate) into
// one LoggedField change. Changes without a timespent item pass as they are.
func foldWorklog(changes []Change) []Change {
	i := slices.IndexFunc(changes, func(ch Change) bool { return ch.Field == "timespent" })
	if i < 0 {
		return changes
	}
	from, _ := strconv.Atoi(changes[i].From)
	to, _ := strconv.Atoi(changes[i].To)
	logged := FormatDuration(to - from)
	if to < from {
		logged = "-" + FormatDuration(from-to)
	}
	out := []Change{{Field: LoggedField, To: logged}}
	for _, ch := range changes {
		if !slices.Contains(worklogFields, ch.Field) {
			out = append(out, ch)
		}
	}
	return out
}

// changesText is changes as "field: from → to", joined by " · ".
func changesText(changes []Change) string {
	parts := make([]string, len(changes))
	for i, ch := range changes {
		if ch.Field == LoggedField {
			parts[i] = LoggedField + " " + ch.To
			continue
		}
		parts[i] = fmt.Sprintf("%s: %s → %s", ch.Field, orDash(ch.From), orDash(ch.To))
	}
	return strings.Join(parts, " · ")
}

// mentions reports whether an ADF body mentions accountID.
func mentions(raw json.RawMessage, accountID string) bool {
	var doc adfNode
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	var walk func(n adfNode) bool
	walk = func(n adfNode) bool {
		if id, _ := n.Attrs["id"].(string); n.Type == "mention" && id == accountID {
			return true
		}
		return slices.ContainsFunc(n.Content, walk)
	}
	return walk(doc)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// History is key's changes and comments by anyone, newest first: its
// latest changelogTail changes and 20 comments.
func (c *Client) History(ctx context.Context, key string) ([]InboxEntry, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	out, err := c.issueActivity(ctx, key, "", time.Time{}, func(string) bool { return true }, "")
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b InboxEntry) int { return b.When.Compare(a.When) })
	return out, nil
}
