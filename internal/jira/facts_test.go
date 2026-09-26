package jira

import (
	"encoding/json"
	"testing"
)

func TestIssueFacts(t *testing.T) {
	values := map[string]json.RawMessage{
		"created":        json.RawMessage(`"2026-09-01T10:00:00.000+0200"`),
		"resolution":     json.RawMessage(`{"name":"Done"}`),
		"resolutiondate": json.RawMessage(`"2026-09-20T10:00:00.000+0200"`),
		"watches":        json.RawMessage(`{"watchCount":4,"isWatching":true}`),
		"votes":          json.RawMessage(`{"votes":2,"hasVoted":false}`),
		"timetracking":   json.RawMessage(`{"timeSpentSeconds":10800,"originalEstimateSeconds":28800,"remainingEstimateSeconds":18000}`),
	}
	f := IssueFacts(values)
	if f.Created.IsZero() || f.Resolved.IsZero() || f.Resolution != "Done" || f.Watchers != 4 || !f.Watching || f.Votes != 2 || f.Voted ||
		f.Spent != 10800 || f.Estimate != 28800 || f.Left != 18000 {
		t.Errorf("facts = %+v", f)
	}
	if f := IssueFacts(nil); f.Watchers != 0 || !f.Created.IsZero() {
		t.Errorf("empty = %+v", f)
	}
}
