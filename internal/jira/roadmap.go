package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The roadmap: a project's epics on a timeline. An epic's dates come from its
// own fields (Start date, or Plans' Target start; Due date, or Target end),
// else from the sprints its children sit in.

// Epic is one roadmap row.
type Epic struct {
	Key, Summary, Status string
	Done                 bool      // its status is in the done category
	Start, End           time.Time // zero when unknown
	// DatesFromSprints marks Start/End taken from the children's sprints.
	DatesFromSprints       bool
	Children, DoneChildren int
	Points, DonePoints     float64
}

// roadmapFieldIDs are the instance's custom fields a roadmap reads.
type roadmapFieldIDs struct {
	start, end, sprint string
}

// roadmapFieldsOf picks the fields out of the field metadata: "Start date"
// before Plans' "Target start", Plans' "Target end" (the due date is a
// system field), and the Agile sprint field.
func roadmapFieldsOf(fields []apiField) roadmapFieldIDs {
	var ids roadmapFieldIDs
	for _, f := range fields {
		name := strings.ToLower(f.Name)
		switch {
		case name == "start date":
			ids.start = f.ID
		case name == "target start" || strings.HasSuffix(f.Schema.Custom, ":jpo-custom-field-baseline-start"):
			if ids.start == "" {
				ids.start = f.ID
			}
		case name == "target end" || strings.HasSuffix(f.Schema.Custom, ":jpo-custom-field-baseline-end"):
			ids.end = f.ID
		case f.Schema.Custom == "com.pyxis.greenhopper.jira:gh-sprint":
			ids.sprint = f.ID
		}
	}
	return ids
}

// apiField is one entry of /rest/api/3/field.
type apiField struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Schema struct {
		Custom string `json:"custom"`
	} `json:"schema"`
}

func (c *Client) resolveRoadmapFields(ctx context.Context) (roadmapFieldIDs, error) {
	c.mu.Lock()
	if c.roadmapFields != nil {
		ids := *c.roadmapFields
		c.mu.Unlock()
		return ids, nil
	}
	c.mu.Unlock()
	var fields []apiField
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/field", "field metadata", nil, &fields); err != nil {
		return roadmapFieldIDs{}, err
	}
	ids := roadmapFieldsOf(fields)
	c.mu.Lock()
	c.roadmapFields = &ids
	c.mu.Unlock()
	return ids, nil
}

// Roadmap lists project's epics in rank order, open ones and those done in
// the last 90 days, with dates and their children's progress.
func (c *Client) Roadmap(ctx context.Context, project string) ([]Epic, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	ids, err := c.resolveRoadmapFields(ctx)
	if err != nil {
		return nil, err
	}
	fields := []string{"summary", "status", "duedate"}
	for _, id := range []string{ids.start, ids.end} {
		if id != "" {
			fields = append(fields, id)
		}
	}
	jql := fmt.Sprintf(`project = %q AND issuetype = Epic AND (statusCategory != Done OR resolved >= -90d) ORDER BY rank`, project)
	raw, err := c.search(ctx, jql, fields)
	if err != nil {
		return nil, err
	}
	epics := make([]Epic, len(raw))
	index := map[string]int{}
	for i, is := range raw {
		e := Epic{Key: is.Key}
		_ = json.Unmarshal(is.Fields["summary"], &e.Summary)
		e.Status, e.Done = statusOf(is.Fields["status"])
		e.Start = dateField(is.Fields[ids.start])
		if e.End = dateField(is.Fields["duedate"]); e.End.IsZero() {
			e.End = dateField(is.Fields[ids.end])
		}
		epics[i] = e
		index[is.Key] = i
	}
	if err := c.addChildren(ctx, epics, index, ids.sprint); err != nil {
		return nil, err
	}
	return epics, nil
}

// addChildren counts each epic's children and points, and fills missing
// dates from the sprints they sit in.
func (c *Client) addChildren(ctx context.Context, epics []Epic, index map[string]int, sprintField string) error {
	sp := c.resolveStoryPointFields(ctx)
	fields := append([]string{"status", "parent"}, sp...)
	if sprintField != "" {
		fields = append(fields, sprintField)
	}
	type span struct{ start, end time.Time }
	spans := make([]span, len(epics))
	keys := make([]string, len(epics))
	for i, e := range epics {
		keys[i] = e.Key
	}
	for chunk := range slices.Chunk(keys, 100) {
		raw, err := c.search(ctx, "parent in ("+strings.Join(chunk, ",")+")", fields)
		if err != nil {
			return err
		}
		for _, is := range raw {
			var p struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(is.Fields["parent"], &p)
			i, ok := index[p.Key]
			if !ok {
				continue
			}
			e := &epics[i]
			_, done := statusOf(is.Fields["status"])
			pts := 0.0
			for _, id := range sp {
				if v, err := strconv.ParseFloat(string(is.Fields[id]), 64); err == nil {
					pts = v
					break
				}
			}
			e.Children++
			e.Points += pts
			if done {
				e.DoneChildren++
				e.DonePoints += pts
			}
			var sprints []struct {
				StartDate time.Time `json:"startDate"`
				EndDate   time.Time `json:"endDate"`
			}
			_ = json.Unmarshal(is.Fields[sprintField], &sprints)
			for _, s := range sprints {
				if !s.StartDate.IsZero() && (spans[i].start.IsZero() || s.StartDate.Before(spans[i].start)) {
					spans[i].start = s.StartDate
				}
				if s.EndDate.After(spans[i].end) {
					spans[i].end = s.EndDate
				}
			}
		}
	}
	for i := range epics {
		e := &epics[i]
		if e.Start.IsZero() && !spans[i].start.IsZero() {
			e.Start, e.DatesFromSprints = day(spans[i].start), true
		}
		if e.End.IsZero() && !spans[i].end.IsZero() {
			e.End, e.DatesFromSprints = day(spans[i].end), true
		}
	}
	return nil
}

// statusOf is a status field's name and whether it is in the done category.
func statusOf(raw json.RawMessage) (string, bool) {
	var s struct {
		Name     string `json:"name"`
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	_ = json.Unmarshal(raw, &s)
	return s.Name, s.Category.Key == "done"
}

// dateField reads a "2006-01-02" field, zero when empty.
func dateField(raw json.RawMessage) time.Time {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return time.Time{}
	}
	d, _ := time.ParseInLocation(time.DateOnly, s, time.Local)
	return d
}

// day is t's calendar day in local time.
func day(t time.Time) time.Time {
	t = t.Local()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}
