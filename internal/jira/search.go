package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// SearchCards returns the cards a JQL search finds, up to the card limit,
// paging by the enhanced search's nextPageToken. Points come from the first
// story point field an issue fills.
func (c *Client) SearchCards(ctx context.Context, jql string) ([]Card, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	sp := c.resolveStoryPointFields(ctx)
	fields := append(strings.Split(cardFields, ","), sp...)
	issues, err := c.search(ctx, jql, fields)
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(issues))
	for _, is := range issues {
		card := toCard(is.Key, is.Fields, "")
		for _, id := range sp {
			if card = toCard(is.Key, is.Fields, id); card.Points != "" {
				break
			}
		}
		out = append(out, card)
	}
	return out, nil
}

// rawIssue is a search hit with its fields undecoded.
type rawIssue struct {
	Key    string                     `json:"key"`
	Fields map[string]json.RawMessage `json:"fields"`
}

// search runs jql up to the card limit, paging by the enhanced search's
// nextPageToken.
func (c *Client) search(ctx context.Context, jql string, fields []string) ([]rawIssue, error) {
	var out []rawIssue
	token := ""
	for len(out) < c.cardLimit {
		body := map[string]any{"jql": jql, "fields": fields, "maxResults": cardPage}
		if token != "" {
			body["nextPageToken"] = token
		}
		var resp struct {
			Issues        []rawIssue `json:"issues"`
			NextPageToken string     `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", "search", body, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Issues...)
		if token = resp.NextPageToken; token == "" || len(resp.Issues) == 0 {
			break
		}
	}
	if len(out) > c.cardLimit {
		out = out[:c.cardLimit]
	}
	return out, nil
}
