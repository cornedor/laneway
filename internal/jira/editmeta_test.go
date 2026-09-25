package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEditMeta: the issue's editable fields beyond the panel's own, sorted,
// with story points, system fields and unwritable shapes left out.
func TestEditMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/ABC-1" || r.URL.Query().Get("expand") != "editmeta" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		io.WriteString(w, `{"fields":{"customfield_2":{"accountId":"a1","displayName":"Ada"}},
		"editmeta":{"fields":{
		  "summary":{"name":"Summary","schema":{"type":"string","system":"summary"}},
		  "customfield_1":{"name":"Story Points","schema":{"type":"number"}},
		  "customfield_2":{"name":"Reviewer","schema":{"type":"user"}},
		  "customfield_3":{"name":"Team","schema":{"type":"option"},"allowedValues":[{"id":"7","value":"Core"}]},
		  "customfield_4":{"name":"Due","schema":{"type":"date"}},
		  "environment":{"name":"Environment","schema":{"type":"string","system":"environment"}}
		}}}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", StoryPointsField: "customfield_1"})
	fields, values, err := c.EditMeta(context.Background(), "ABC-1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fields {
		got = append(got, f.Name+":"+f.Kind)
	}
	want := []string{"Environment:doc", "Reviewer:user", "Team:option"}
	if len(got) != len(want) {
		t.Fatalf("fields = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fields = %v", got)
		}
	}
	if v := DecodeValue(KindUser, values["customfield_2"]); len(v.Users) != 1 || v.Users[0].DisplayName != "Ada" {
		t.Errorf("reviewer value = %+v", v)
	}
	if fields[2].Options[0].Name != "Core" {
		t.Errorf("options = %+v", fields[2].Options)
	}
}

// TestSetField: one field in a PUT, nil clears.
func TestSetField(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if err := c.SetField(context.Background(), "ABC-1", "customfield_3", nil); err != nil {
		t.Fatal(err)
	}
	if body != `{"fields":{"customfield_3":null}}` {
		t.Errorf("body = %s", body)
	}
}
