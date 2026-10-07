package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sprint"):
			io.WriteString(w, `{"isLast":true,"values":[{"id":1,"name":"S1","startDate":"2026-08-01T00:00:00Z","completeDate":"2026-08-14T00:00:00Z"},
				{"id":2,"name":"S2","startDate":"2026-09-01T00:00:00Z","completeDate":"2026-09-14T00:00:00Z"}]}`)
		case r.URL.Path == "/rest/api/3/status":
			io.WriteString(w, `[{"id":"1","statusCategory":{"key":"new"}},{"id":"3","statusCategory":{"key":"indeterminate"}},{"id":"5","statusCategory":{"key":"done"}}]`)
		case r.URL.Path == "/rest/api/3/field":
			io.WriteString(w, `[]`)
		default: // the sprint's issues
			io.WriteString(w, `{"issues":[
				{"key":"A-1","fields":{"resolutiondate":"2026-09-10T00:00:00.000+0000","status":{"id":"5"}},"changelog":{"histories":[
					{"created":"2026-09-05T00:00:00.000+0000","items":[{"field":"status","from":"3","to":"1"}]}]}},
				{"key":"A-2","fields":{"status":{"id":"3"}},"changelog":{"histories":[
					{"created":"2026-09-03T00:00:00.000+0000","items":[{"field":"Sprint","from":"","to":"2"}]}]}}]}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.Retro(context.Background(), 1, 2, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s2 := got[1]
	if len(got) != 2 || s2.Name != "S2" || strings.Join(s2.Done, ",") != "A-1" || strings.Join(s2.Carried, ",") != "A-2" || strings.Join(s2.Back, ",") != "A-1" {
		t.Errorf("S2 = %+v", s2)
	}
	// Done by the Doing line (A-1's history ends in to do), with Done as
	// a second line: A-2 is past Doing, not Done.
	cols := []Column{{Name: "To Do", StatusIDs: []string{"1"}}, {Name: "Doing", StatusIDs: []string{"3"}}, {Name: "Done", StatusIDs: []string{"5"}}}
	got, err = c.Retro(context.Background(), 1, 2, "", NewLine(cols, "Doing", false), NewLine(cols, "Done", false))
	if err != nil {
		t.Fatal(err)
	}
	if s2 := got[1]; strings.Join(s2.Done, ",") != "A-2" || strings.Join(s2.Carried, ",") != "A-1" || strings.Join(s2.Between, ",") != "A-2" {
		t.Errorf("S2 by lines = %+v", s2)
	}
}
