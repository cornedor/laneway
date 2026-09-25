package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestCardsPagesAndDecodes: the board issue list is paged until the total, and
// each issue flattens into a card with the board's points field.
func TestCardsPagesAndDecodes(t *testing.T) {
	var jqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/agile/1.0/board/7/issue" {
			t.Errorf("path = %q", r.URL.Path)
		}
		jqls = append(jqls, r.URL.Query().Get("jql"))
		start, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total": 2, "issues": [{"key": "ABC-%d", "fields": {
			"summary": "Card %d", "status": {"id": "3", "name": "In progress"},
			"issuetype": {"name": "Bug"}, "assignee": {"accountId": "a1", "displayName": "Ada"},
			"customfield_1": 2.5}}]}`, start+1, start+1)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cards, total, err := c.BoardIssues(context.Background(), 7, "status != Done", "customfield_1")
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(cards) != 2 || cards[1].Key != "ABC-2" {
		t.Fatalf("total=%d cards=%+v", total, cards)
	}
	want := Card{Key: "ABC-1", Summary: "Card 1", Type: "Bug", Status: "In progress", StatusID: "3", Assignee: "Ada", AssigneeID: "a1", Points: "2.5"}
	if cards[0] != want {
		t.Errorf("card = %+v, want %+v", cards[0], want)
	}
	if jqls[0] != "status != Done" {
		t.Errorf("jql = %q", jqls[0])
	}
}

func TestBoardConfiguration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"columnConfig": {"columns": [
			{"name": "To do", "statuses": [{"id": "1"}, {"id": "2"}]},
			{"name": "Done", "statuses": [{"id": "5"}]}]},
			"estimation": {"type": "field", "field": {"fieldId": "customfield_9"}}}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cfg, err := c.BoardConfiguration(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PointsField != "customfield_9" || len(cfg.Columns) != 2 || len(cfg.Columns[0].StatusIDs) != 2 {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestTransitionMetaAndRules(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/ABC-1/transitions" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&sent)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/rest/api/3/issue/ABC-1/transitions":
			_, _ = w.Write([]byte(`{"transitions": [{"id": "15", "name": "Code review", "hasScreen": true,
				"to": {"id": "10004", "name": "Code review"},
				"fields": {"customfield_2": {"name": "Code Reviewer", "schema": {"type": "user"}},
					"customfield_3": {"name": "Tester", "schema": {"type": "array", "items": "user"}},
					"customfield_4": {"name": "Info", "schema": {"type": "string", "custom": "x:textarea"}}}}]}`))
		case r.URL.Path == "/rest/api/3/project/ABC":
			_, _ = w.Write([]byte(`{"id": "10"}`))
		case r.URL.Path == "/rest/api/3/workflowscheme/project":
			_, _ = w.Write([]byte(`{"values": [{"workflowScheme": {"defaultWorkflow": "W", "issueTypeMappings": {"5": "Epic W"}}}]}`))
		case r.URL.Path == "/rest/api/3/workflow/search":
			if r.URL.Query().Get("workflowName") != "W" {
				t.Errorf("workflow = %q, want W", r.URL.Query().Get("workflowName"))
			}
			_, _ = w.Write([]byte(`{"values": [{"id": {"name": "W"}, "transitions": [{"id": "15", "rules": {"validators": [
				{"type": "FieldRequiredValidator", "configuration": {"fields": ["customfield_2"], "errorMessage": "Vul de code reviewer in."}},
				{"type": "SomethingElse", "configuration": {}}]}}]}]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	ctx := context.Background()

	metas, err := c.TransitionsMeta(ctx, "ABC-1")
	if err != nil || len(metas) != 1 {
		t.Fatalf("metas = %+v, err %v", metas, err)
	}
	kinds := map[string]string{}
	for _, f := range metas[0].Fields {
		kinds[f.Name] = f.Kind
	}
	if kinds["Code Reviewer"] != KindUser || kinds["Tester"] != KindUsers || kinds["Info"] != KindDoc {
		t.Errorf("kinds = %v", kinds)
	}

	rules, err := c.TransitionRules(ctx, "ABC", "1")
	if err != nil {
		t.Fatal(err)
	}
	if r := rules["15"]; len(r.Required) != 1 || r.Required[0] != "customfield_2" || r.Message != "Vul de code reviewer in." {
		t.Errorf("rule = %+v", r)
	}

	v, _, _ := EncodeValue(KindUser, Value{Users: []User{{AccountID: "a1"}}})
	if err := c.TransitionWith(ctx, "ABC-1", "15", map[string]any{"customfield_2": v}, "why"); err != nil {
		t.Fatal(err)
	}
	if sent["transition"].(map[string]any)["id"] != "15" || sent["fields"].(map[string]any)["customfield_2"].(map[string]any)["accountId"] != "a1" {
		t.Errorf("sent = %v", sent)
	}
	if _, ok := sent["update"].(map[string]any)["comment"]; !ok {
		t.Errorf("no comment in %v", sent)
	}
}

func TestDecodeValue(t *testing.T) {
	if v := DecodeValue(KindUsers, json.RawMessage(`[{"accountId": "a", "displayName": "A"}]`)); len(v.Users) != 1 || v.Empty() {
		t.Errorf("users = %+v", v)
	}
	doc := `{"type": "doc", "version": 1, "content": [{"type": "paragraph", "content": [{"type": "text", "text": "steps"}]}]}`
	if v := DecodeValue(KindDoc, json.RawMessage(doc)); v.Text != "steps" {
		t.Errorf("doc = %q", v.Text)
	}
	if !DecodeValue(KindUser, json.RawMessage(`null`)).Empty() {
		t.Error("null not empty")
	}
}
