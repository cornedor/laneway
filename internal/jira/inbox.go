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
	"sync"
	"time"
)

// The inbox: what others did since you last looked, on the issues you watch,
// are assigned or reported — field changes, comments, and comments that
// mention you.

// inboxIssues caps how many recently updated issues the inbox reads, unless
// Config.InboxIssues says otherwise.
const inboxIssues = 30

// InboxEntry is one thing that happened on an issue.
type InboxEntry struct {
	Key, Summary string
	When         time.Time
	Who          string
	What         string // "Status: To Do → Done", "commented: …"
	Mention      bool   // a comment that mentions you
}

// Inbox lists what others did since since, mentions first, then newest.
func (c *Client) Inbox(ctx context.Context, since time.Time) ([]InboxEntry, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	me, err := c.Myself(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := c.search(ctx, inboxJQL(since)+" ORDER BY updated DESC", []string{"summary"})
	if err != nil {
		return nil, err
	}
	issues = issues[:min(len(issues), c.inboxCap)]
	var (
		out  []InboxEntry
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs = make([]error, len(issues))
	)
	for i, is := range issues {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var summary string
			_ = json.Unmarshal(is.Fields["summary"], &summary)
			entries, err := c.issueActivity(ctx, is.Key, summary, since, func(who string) bool { return who != me.AccountID }, me.AccountID)
			mu.Lock()
			out = append(out, entries...)
			mu.Unlock()
			errs[i] = err
		}()
	}
	wg.Wait()
	if err := firstError(errs); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b InboxEntry) int {
		if a.Mention != b.Mention {
			if a.Mention {
				return -1
			}
			return 1
		}
		return b.When.Compare(a.When)
	})
	return out, nil
}

// inboxJQL finds your issues updated since since. Relative minutes sidestep
// the profile time zone JQL dates are read in.
func inboxJQL(since time.Time) string {
	mins := int(math.Ceil(time.Since(since).Minutes())) + 1
	return fmt.Sprintf("(watcher = currentUser() OR assignee = currentUser() OR reporter = currentUser()) AND updated >= -%dm", mins)
}

// InboxCount is how many of your issues others updated since since and you
// did not touch after: one search, for a badge. An issue you and someone
// else both changed is left out.
func (c *Client) InboxCount(ctx context.Context, since time.Time) (int, error) {
	if !c.Enabled() {
		return 0, errNotConfigured
	}
	// updatedBy takes a user, not currentUser(): JQL won't nest functions.
	me, err := c.Myself(ctx)
	if err != nil {
		return 0, err
	}
	mins := int(math.Ceil(time.Since(since).Minutes())) + 1
	jql := fmt.Sprintf(`%s AND issue not in updatedBy("%s", "-%dm")`, inboxJQL(since), me.AccountID, mins)
	issues, err := c.search(ctx, jql, []string{"summary"})
	return len(issues), err
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
		text := strings.Join(strings.Fields(adfToMarkdown(cm.Body)), " ")
		mention := mentions(cm.Body, me)
		what := "commented: " + text
		if mention {
			what = "mentioned you: " + text
		}
		out = append(out, InboxEntry{Key: key, Summary: summary, When: when, Who: cm.Author.DisplayName, What: what, Mention: mention})
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
		var parts []string
		for _, it := range h.Items {
			parts = append(parts, fmt.Sprintf("%s: %s → %s", it.Field, orDash(it.FromString), orDash(it.ToString)))
		}
		if len(parts) > 0 {
			out = append(out, InboxEntry{Key: key, Summary: summary, When: when, Who: h.Author.DisplayName, What: strings.Join(parts, " · ")})
		}
	}
	return out, nil
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
