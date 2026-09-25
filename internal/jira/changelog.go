package jira

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// changelogTail is how many of an issue's newest changes ChangeAuthor reads.
const changelogTail = 50

// ChangeAuthor is the accountId of whoever last changed fieldID on key
// ("status", "assignee", a points customfield, …), or of its creator for
// fieldID "". "" with no error when no such change is on record.
func (c *Client) ChangeAuthor(ctx context.Context, key, fieldID string) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
	}
	base := "/rest/api/3/issue/" + url.PathEscape(key)
	if fieldID == "" {
		var resp struct {
			Fields struct {
				Creator struct {
					AccountID string `json:"accountId"`
				} `json:"creator"`
			} `json:"fields"`
		}
		err := c.do(ctx, http.MethodGet, base+"?fields=creator", "issue creator", nil, &resp)
		return resp.Fields.Creator.AccountID, err
	}
	type page struct {
		Total  int `json:"total"`
		Values []struct {
			Author struct {
				AccountID string `json:"accountId"`
			} `json:"author"`
			Items []struct {
				FieldID string `json:"fieldId"`
			} `json:"items"`
		} `json:"values"`
	}
	get := func(start, n int) (page, error) {
		var p page
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {strconv.Itoa(n)}}
		err := c.do(ctx, http.MethodGet, base+"/changelog?"+q.Encode(), "changelog", nil, &p)
		return p, err
	}
	// The changelog pages oldest first: learn the total, then read the tail.
	p, err := get(0, 1)
	if err == nil && p.Total > 1 {
		p, err = get(max(p.Total-changelogTail, 0), changelogTail)
	}
	if err != nil {
		return "", err
	}
	for i := len(p.Values) - 1; i >= 0; i-- {
		for _, it := range p.Values[i].Items {
			if it.FieldID == fieldID {
				return p.Values[i].Author.AccountID, nil
			}
		}
	}
	return "", nil
}
