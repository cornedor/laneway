package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueLinks(t *testing.T) {
	body := `{"key":"ABC-2","fields":{"summary":"s",
		"parent":{"key":"ABC-1","fields":{"summary":"Epic","status":{"name":"Open"}}},
		"issuelinks":[
			{"type":{"inward":"is blocked by","outward":"blocks"},"outwardIssue":{"key":"XYZ-9","fields":{"summary":"Other","status":{"name":"Done"}}}},
			{"type":{"inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"XYZ-3","fields":{"summary":"Before"}}}],
		"subtasks":[{"key":"ABC-5","fields":{"summary":"Part"}}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	iss, err := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t"}).Get(context.Background(), "ABC-2")
	if err != nil {
		t.Fatal(err)
	}
	want := []Link{
		{Rel: "parent", Key: "ABC-1", Summary: "Epic", Status: "Open"},
		{Rel: "blocks", Key: "XYZ-9", Summary: "Other", Status: "Done"},
		{Rel: "is blocked by", Key: "XYZ-3", Summary: "Before"},
		{Rel: "subtask", Key: "ABC-5", Summary: "Part"},
	}
	if len(iss.Links) != len(want) {
		t.Fatalf("links = %+v", iss.Links)
	}
	for i := range want {
		if iss.Links[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, iss.Links[i], want[i])
		}
	}
}

func TestCardParent(t *testing.T) {
	f := map[string]json.RawMessage{
		"summary": json.RawMessage(`"s"`),
		"parent":  json.RawMessage(`{"key":"ABC-1","fields":{"summary":"Epic"}}`),
	}
	c := toCard("ABC-2", f, "")
	if c.ParentKey != "ABC-1" || c.ParentSummary != "Epic" {
		t.Errorf("parent = %q %q", c.ParentKey, c.ParentSummary)
	}
	if c := toCard("ABC-3", map[string]json.RawMessage{}, ""); c.ParentKey != "" {
		t.Errorf("no parent field gave %q", c.ParentKey)
	}
}

func TestCardReporterComponents(t *testing.T) {
	f := map[string]json.RawMessage{
		"created":    json.RawMessage(`"2026-09-01T10:00:00.000+0200"`),
		"reporter":   json.RawMessage(`{"accountId":"b1","displayName":"Bob"}`),
		"components": json.RawMessage(`[{"id":"1","name":"Web shop"},{"id":"2","name":"API"}]`),
	}
	c := toCard("ABC-2", f, "")
	if c.Reporter != "Bob" || c.Components != "Web shop"+ExtraSep+"API" || c.Created.IsZero() {
		t.Errorf("card = %q %q %v", c.Reporter, c.Components, c.Created)
	}
}
