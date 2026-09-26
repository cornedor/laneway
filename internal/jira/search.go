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
	dev, flag := c.devField(ctx), c.flagField(ctx)
	more := c.moreCardFields(ctx)
	for _, id := range append([]string{dev, flag}, more.ids()...) {
		if id != "" {
			fields = append(fields, id)
		}
	}
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
		card.PR = prState(is.Fields[dev])
		card.Deploy = deployEnv(is.Fields[dev])
		card.Flagged = flagSet(is.Fields[flag])
		more.fill(&card, is.Fields)
		out = append(out, card)
	}
	return out, nil
}

// rawIssue is a search hit with its fields undecoded.
type rawIssue struct {
	Key       string                     `json:"key"`
	Fields    map[string]json.RawMessage `json:"fields"`
	Changelog struct {
		Histories []struct {
			Created string `json:"created"`
			Items   []struct {
				Field string `json:"field"`
				From  string `json:"from"`
				To    string `json:"to"`
			} `json:"items"`
		} `json:"histories"`
	} `json:"changelog"` // with expand "changelog" only
}

// search runs jql up to the card limit, paging by the enhanced search's
// nextPageToken.
func (c *Client) search(ctx context.Context, jql string, fields []string) ([]rawIssue, error) {
	return c.searchExpand(ctx, jql, fields, "")
}

// searchExpand is search with expand ("changelog"), "" for none.
func (c *Client) searchExpand(ctx context.Context, jql string, fields []string, expand string) ([]rawIssue, error) {
	var out []rawIssue
	token := ""
	for len(out) < c.cardLimit {
		body := map[string]any{"jql": jql, "fields": fields, "maxResults": cardPage}
		if expand != "" {
			body["expand"] = expand
		}
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

// FindIssues is a text search over every issue you can see: summary,
// description and comments, words as typed prefixes; one page of n,
// recently updated first.
func (c *Client) FindIssues(ctx context.Context, text string, n int) ([]Card, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	clean := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' {
			return ' '
		}
		return r
	}, strings.TrimSpace(text))
	if clean == "" {
		return nil, nil
	}
	var words []string
	for _, w := range strings.Fields(clean) {
		words = append(words, w+"*")
	}
	body := map[string]any{"jql": `text ~ "` + strings.Join(words, " ") + `" ORDER BY updated DESC`,
		"fields": strings.Split(cardFields, ","), "maxResults": n}
	var resp struct {
		Issues []rawIssue `json:"issues"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", "search", body, &resp); err != nil {
		return nil, err
	}
	out := make([]Card, len(resp.Issues))
	for i, is := range resp.Issues {
		out[i] = toCard(is.Key, is.Fields, "")
	}
	return out, nil
}
