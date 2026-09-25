package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCreateIssue(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"id":"1","key":"JB-42"}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})

	key, err := c.CreateIssue(context.Background(), NewIssue{
		Project: "JB", Type: "Bug", Summary: " Broken cart ", Description: "It breaks.", Labels: []string{"Frontend"},
	})
	if err != nil || key != "JB-42" {
		t.Fatalf("CreateIssue = %q, %v", key, err)
	}
	f := got["fields"].(map[string]any)
	if f["summary"] != "Broken cart" {
		t.Errorf("summary = %v", f["summary"])
	}
	if f["project"].(map[string]any)["key"] != "JB" || f["issuetype"].(map[string]any)["name"] != "Bug" {
		t.Errorf("project/type = %v / %v", f["project"], f["issuetype"])
	}
	if f["description"].(map[string]any)["type"] != "doc" {
		t.Errorf("description not ADF: %v", f["description"])
	}
	if !reflect.DeepEqual(f["labels"], []any{"Frontend"}) {
		t.Errorf("labels = %v", f["labels"])
	}

	if _, err := c.CreateIssue(context.Background(), NewIssue{Project: "JB", Type: "Bug"}); err == nil {
		t.Error("empty summary should fail before calling Jira")
	}
}

func TestRecentLabelsMostUsedFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[
			{"fields":{"labels":["Backend"]}},
			{"fields":{"labels":["Frontend"]}},
			{"fields":{"labels":["Frontend","Backend"]}},
			{"fields":{"labels":["Frontend"]}}]}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.RecentLabels(context.Background(), "JB")
	if err != nil || !reflect.DeepEqual(got, []string{"Frontend", "Backend"}) {
		t.Fatalf("RecentLabels = %v, %v", got, err)
	}
}

func TestIssueTypesSkipsSubtasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/createmeta/JB/issuetypes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"issueTypes":[{"id":"1","name":"Bug"},{"id":"2","name":"Sub-task","subtask":true}]}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.IssueTypes(context.Background(), "JB")
	if err != nil || len(got) != 1 || got[0].Name != "Bug" {
		t.Fatalf("IssueTypes = %v, %v", got, err)
	}
}
