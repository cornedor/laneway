package jira

import (
	"encoding/json"
	"time"
)

// Facts are the issue's read-only details the panel shows beside its
// fields, read from the values EditMeta fetched: when it was made and
// resolved, who watches and votes, and its time tracking.
type Facts struct {
	Created, Resolved     time.Time
	Resolution            string
	Watchers, Votes       int
	Watching, Voted       bool
	Spent, Estimate, Left int // time tracking, in seconds; 0 for none
}

// IssueFacts reads Facts from an issue's field values.
func IssueFacts(values map[string]json.RawMessage) Facts {
	var f Facts
	str := func(name string) string {
		var s string
		_ = json.Unmarshal(values[name], &s)
		return s
	}
	f.Created, _ = time.Parse(jiraTime, str("created"))
	f.Resolved, _ = time.Parse(jiraTime, str("resolutiondate"))
	var res struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(values["resolution"], &res)
	f.Resolution = res.Name
	var w struct {
		Count int  `json:"watchCount"`
		Mine  bool `json:"isWatching"`
	}
	_ = json.Unmarshal(values["watches"], &w)
	f.Watchers, f.Watching = w.Count, w.Mine
	var v struct {
		Count int  `json:"votes"`
		Mine  bool `json:"hasVoted"`
	}
	_ = json.Unmarshal(values["votes"], &v)
	f.Votes, f.Voted = v.Count, v.Mine
	var tt struct {
		Spent    int `json:"timeSpentSeconds"`
		Estimate int `json:"originalEstimateSeconds"`
		Left     int `json:"remainingEstimateSeconds"`
	}
	_ = json.Unmarshal(values["timetracking"], &tt)
	f.Spent, f.Estimate, f.Left = tt.Spent, tt.Estimate, tt.Left
	return f
}
