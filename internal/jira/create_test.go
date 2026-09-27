package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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

func TestProjectStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/project/JB/statuses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"name":"Taak","subtask":false,"statuses":[{"name":"Te doen","statusCategory":{"key":"new"}},{"name":"Klaar","statusCategory":{"key":"done"}}]}]`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.ProjectStatuses(context.Background(), "JB")
	want := []TypeStatuses{{Type: "Taak", Statuses: []ProjectStatus{{"Te doen", "new"}, {"Klaar", "done"}}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectStatuses = %+v, %v", got, err)
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

func TestCreateFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/createmeta/JB/issuetypes":
			_, _ = w.Write([]byte(`{"issueTypes":[{"id":"1","name":"Bug"},{"id":"2","name":"Sub-task","subtask":true}]}`))
		case "/rest/api/3/issue/createmeta/JB/issuetypes/1":
			_, _ = w.Write([]byte(`{"fields":[
				{"fieldId":"components","name":"Components","required":true,"schema":{"type":"array","items":"component","system":"components"},"allowedValues":[{"id":"10","name":"Web"}]},
				{"fieldId":"reporter","name":"Reporter","required":true,"hasDefaultValue":true,"schema":{"type":"user","system":"reporter"}},
				{"fieldId":"labels","name":"Labels","required":false,"schema":{"type":"array","items":"string"}}]}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.CreateFields(context.Background(), "JB", "bug")
	if err != nil || len(got) != 3 {
		t.Fatalf("CreateFields = %+v, %v", got, err)
	}
	if !got[0].Required || got[0].Kind != KindOptions || got[0].Options[0].Name != "Web" {
		t.Errorf("components = %+v", got[0])
	}
	if got[1].Required || got[2].Required {
		t.Errorf("reporter has a default and labels are optional: %+v %+v", got[1], got[2])
	}
	if _, err := c.CreateFields(context.Background(), "JB", "Epic"); err == nil {
		t.Error("an unknown type should fail")
	}
}

// TestCreateIssueMentions: "@Name" in the description becomes a mention.
func TestCreateIssueMentions(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"key":"JB-43"}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if _, err := c.CreateIssue(context.Background(), NewIssue{Project: "JB", Type: "Bug", Summary: "x",
		Description: "ask @Ada first", Mentions: []Mention{{AccountID: "a1", DisplayName: "Ada"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"type":"mention"`) || !strings.Contains(body, `"id":"a1"`) {
		t.Errorf("body %s", body)
	}
}

// TestCommentVisibility: an internal note and a role reach the comment.
func TestCommentVisibility(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/project/SD":
			io.WriteString(w, `{"projectTypeKey":"service_desk"}`)
		case "/rest/api/3/project/SD/role":
			io.WriteString(w, `{"Developers":"u1","Administrators":"u2"}`)
		default:
			b, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(b))
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	vis, err := c.CommentVisibilities(context.Background(), "SD")
	if err != nil || len(vis) != 3 || !vis[0].Internal || vis[1].Role != "Administrators" {
		t.Fatalf("visibilities %v %v", vis, err)
	}
	_ = c.AddCommentMentions(context.Background(), "SD-1", "hi", nil, nil, vis[0])
	_ = c.AddCommentMentions(context.Background(), "SD-1", "hi", nil, nil, vis[2])
	if !strings.Contains(bodies[0], `"sd.public.comment"`) || !strings.Contains(bodies[0], `"internal":true`) ||
		!strings.Contains(bodies[1], `"visibility":{"type":"role","value":"Developers"}`) {
		t.Errorf("bodies %q", bodies)
	}
}
