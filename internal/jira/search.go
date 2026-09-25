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
	var out []Card
	token := ""
	for len(out) < c.cardLimit {
		body := map[string]any{"jql": jql, "fields": fields, "maxResults": cardPage}
		if token != "" {
			body["nextPageToken"] = token
		}
		var resp struct {
			Issues []struct {
				Key    string                     `json:"key"`
				Fields map[string]json.RawMessage `json:"fields"`
			} `json:"issues"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", "search", body, &resp); err != nil {
			return nil, err
		}
		for _, is := range resp.Issues {
			card := toCard(is.Key, is.Fields, "")
			for _, id := range sp {
				if card = toCard(is.Key, is.Fields, id); card.Points != "" {
					break
				}
			}
			out = append(out, card)
		}
		if token = resp.NextPageToken; token == "" || len(resp.Issues) == 0 {
			break
		}
	}
	if len(out) > c.cardLimit {
		out = out[:c.cardLimit]
	}
	return out, nil
}
