package jira

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// JQL autocomplete: the fields, functions and keywords a query can use, and
// suggestions for a field's values, as Jira's own search box gets them.

// JQLWords are what a JQL clause can start with, and the keywords.
type JQLWords struct {
	Fields    []string // as written in JQL: status, assignee, "Story Points"
	Functions []string // currentUser(), openSprints(), …
	Reserved  []string // AND, OR, NOT, ORDER BY, …
}

// JQLAutocomplete lists the instance's JQL fields, functions and keywords,
// cached for the session.
func (c *Client) JQLAutocomplete(ctx context.Context) (JQLWords, error) {
	if !c.Enabled() {
		return JQLWords{}, errNotConfigured
	}
	bm := &c.boardMeta
	words, err := cached(&bm.mu, &bm.jql, "", func() ([]JQLWords, error) {
		var resp struct {
			Fields []struct {
				Value string `json:"value"`
			} `json:"visibleFieldNames"`
			Functions []struct {
				Value string `json:"value"`
			} `json:"visibleFunctionNames"`
			Reserved []string `json:"jqlReservedWords"`
		}
		if err := c.do(ctx, http.MethodGet, "/rest/api/3/jql/autocompletedata", "jql fields", nil, &resp); err != nil {
			return nil, err
		}
		var w JQLWords
		for _, f := range resp.Fields {
			w.Fields = append(w.Fields, f.Value)
		}
		for _, f := range resp.Functions {
			w.Functions = append(w.Functions, f.Value)
		}
		w.Reserved = resp.Reserved
		return []JQLWords{w}, nil
	})
	if err != nil || len(words) == 0 {
		return JQLWords{}, err
	}
	return words[0], nil
}

// JQLValues suggests values of field starting like prefix.
func (c *Client) JQLValues(ctx context.Context, field, prefix string) ([]string, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp struct {
		Results []struct {
			Value string `json:"value"`
		} `json:"results"`
	}
	q := url.Values{"fieldName": {strings.Trim(field, `"`)}, "fieldValue": {prefix}}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/jql/autocompletedata/suggestions?"+q.Encode(), "jql values", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, r.Value)
	}
	return out, nil
}
