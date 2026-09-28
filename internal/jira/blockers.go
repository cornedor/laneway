package jira

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
)

// Blockers is each key's open blockers (issues it "is blocked by" that are
// not done), in link order; keys with none are left out.
func (c *Client) Blockers(ctx context.Context, keys []string) (map[string][]string, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	out := map[string][]string{}
	for chunk := range slices.Chunk(keys, 100) {
		raw, err := c.searchUpTo(ctx, "key in ("+strings.Join(chunk, ",")+")", []string{"issuelinks"}, "", len(chunk))
		if err != nil {
			return nil, err
		}
		for _, is := range raw {
			var links []struct {
				Type struct {
					Inward string `json:"inward"`
				} `json:"type"`
				InwardIssue *struct {
					Key    string `json:"key"`
					Fields struct {
						Status struct {
							Category struct {
								Key string `json:"key"`
							} `json:"statusCategory"`
						} `json:"status"`
					} `json:"fields"`
				} `json:"inwardIssue"`
			}
			_ = json.Unmarshal(is.Fields["issuelinks"], &links)
			for _, l := range links {
				if b := l.InwardIssue; b != nil && strings.EqualFold(l.Type.Inward, "is blocked by") &&
					b.Fields.Status.Category.Key != "done" && ValidKey(b.Key) {
					out[is.Key] = append(out[is.Key], b.Key)
				}
			}
		}
	}
	return out, nil
}
