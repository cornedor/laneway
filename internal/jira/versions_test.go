package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/project/ABC/version" || r.URL.Query().Get("expand") != "issuesstatus" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"values":[
			{"id":"11","name":"1.1","released":false,"issuesStatusForFixVersion":{"unmapped":1,"toDo":2,"inProgress":1,"done":4}},
			{"id":"10","name":"1.0","released":true,"releaseDate":"2026-09-01","issuesStatusForFixVersion":{"done":3}}
		],"isLast":true}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	vs, err := c.Versions(context.Background(), "ABC")
	if err != nil {
		t.Fatal(err)
	}
	want := []Version{
		{ID: "11", Name: "1.1", Done: 4, Total: 8},
		{ID: "10", Name: "1.0", Released: true, ReleaseDate: "2026-09-01", Done: 3, Total: 3},
	}
	if fmt.Sprint(vs) != fmt.Sprint(want) {
		t.Errorf("versions = %+v", vs)
	}
}

func TestReleaseVersion(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/version/11" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if err := c.ReleaseVersion(context.Background(), "11", time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if body["released"] != true || body["releaseDate"] != "2026-09-27" {
		t.Errorf("body = %v", body)
	}
}
