package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memMeta map[string]string

func (m memMeta) GetMeta(k string) (string, bool, error) { v, ok := m[k]; return v, ok, nil }
func (m memMeta) SetMeta(k, v string) error              { m[k] = v; return nil }

// TestLayoutFillsClosedIssue: an open issue's edit screen is kept by project
// and type; a closed one (Jira offers none) gets it back read-only, with
// its values.
func TestLayoutFillsClosedIssue(t *testing.T) {
	open := `{"fields":{"project":{"key":"ABC"},"issuetype":{"id":"10001"},"customfield_1":"Ada"},
		"editmeta":{"fields":{"customfield_1":{"name":"Tester","schema":{"type":"string"}}}}}`
	closed := `{"fields":{"project":{"key":"ABC"},"issuetype":{"id":"10001"},"customfield_1":"Bob"},"editmeta":{"fields":{}}}`
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/ABC-1":
			_, _ = w.Write([]byte(open))
		case "/rest/api/3/issue/ABC-2":
			_, _ = w.Write([]byte(closed))
		case "/rest/api/3/issue/ABC-3":
			asked = r.URL.Query().Get("fields")
			_, _ = w.Write([]byte(`{"key":"ABC-3","fields":{"summary":"s","issuetype":{"id":"10001","name":"Story"},"customfield_1":"Cy"}}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t"})
	ctx := context.Background()
	if f, _, _ := c.EditMeta(ctx, "ABC-2"); len(f) != 0 {
		t.Fatalf("without a store: %+v", f)
	}
	c.SetLayouts(memMeta{})
	if f, _, err := c.EditMeta(ctx, "ABC-1"); err != nil || len(f) != 1 || f[0].ReadOnly {
		t.Fatalf("open: %+v %v", f, err)
	}
	f, raw, err := c.EditMeta(ctx, "ABC-2")
	if err != nil || len(f) != 1 || f[0].Name != "Tester" || !f[0].ReadOnly || DecodeValue(f[0].Kind, raw[f[0].ID]).Text != "Bob" {
		t.Fatalf("closed: %+v %v", f, err)
	}
	// The next issue of the project asks for the screen's fields and carries
	// them, so the panel draws them at once.
	iss, err := c.Get(ctx, "ABC-3")
	if err != nil || !strings.Contains(asked, "customfield_1") || len(iss.Screen) != 1 || iss.Screen[0].ReadOnly || iss.ScreenValues["customfield_1"].Text != "Cy" {
		t.Fatalf("screen: %q %+v %v", asked, iss, err)
	}
}
