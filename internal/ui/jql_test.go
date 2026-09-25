package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// findMsg runs cmd, and a batch's commands, until one yields a T. Ticks
// (which would wait) are not expected in what it runs.
func findMsg[T any](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	switch msg := cmd().(type) {
	case T:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if v, ok := findMsg[T](c); ok {
				return v, true
			}
		}
	}
	return zero, false
}

func TestJQLContext(t *testing.T) {
	for _, c := range []struct {
		in, field, prefix string
		value             bool
	}{
		{"sta", "", "sta", false},
		{"project = ABC AND ass", "", "ass", false},
		{"status = ", "status", "", true},
		{"status = In", "status", "In", true},
		{`status = "In Pro`, "status", "In Pro", true},
		{"status not in (Done, Clo", "status", "Clo", true},
		{"assignee is not ", "assignee", "", true},
		{"status in (Done) AND ", "", "", false},
		{"text ~ ", "text", "", true},
	} {
		field, prefix, _, value := jqlContext(c.in)
		if field != c.field || prefix != c.prefix || value != c.value {
			t.Errorf("%q: field %q prefix %q value %v", c.in, field, prefix, value)
		}
	}
	if got := jqlComplete("status = In", "In Progress"); got != `status = "In Progress" ` {
		t.Errorf("complete = %q", got)
	}
	if got := jqlComplete("project = ABC AND ass", "assignee"); got != "project = ABC AND assignee " {
		t.Errorf("complete = %q", got)
	}
	if got := jqlMatches([]string{"status", "assignee", "statusCategory", "lastViewed"}, "stat"); !slices.Equal(got, []string{"status", "statusCategory"}) {
		t.Errorf("matches = %v", got)
	}
}

// TestJQLSearch: Q opens the input; fields complete locally, values come
// from Jira; enter shows the search as a view.
func TestJQLSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/jql/autocompletedata":
			io.WriteString(w, `{"visibleFieldNames":[{"value":"status"},{"value":"assignee"}],"visibleFunctionNames":[{"value":"currentUser()"}],"jqlReservedWords":["and","order"]}`)
		case "/rest/api/3/jql/autocompletedata/suggestions":
			if r.URL.Query().Get("fieldName") != "status" {
				t.Errorf("suggestions for %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"results":[{"value":"In Progress"},{"value":"Done"}]}`)
		default:
			io.WriteString(w, `{"issues":[]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "Q"))
	m = out.(Model)
	if m.jql == nil || !strings.Contains(m.View().Content, "JQL search") {
		t.Fatal("Q should open the JQL input")
	}
	out, _ = m.handleJQLWords(cmd().(jqlWordsMsg))
	m = out.(Model)
	for _, k := range []string{"s", "t"} {
		out, _ = m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	if !slices.Equal(m.jql.sugg, []string{"status"}) {
		t.Fatalf("sugg = %v", m.jql.sugg)
	}
	out, _ = m.handleKey(keyMsg(t, "tab"))
	m = out.(Model)
	for _, k := range []string{"=", "space"} {
		out, cmd = m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	if m.jql.input.Value() != "status = " || cmd == nil {
		t.Fatalf("value %q", m.jql.input.Value())
	}
	vals, ok := findMsg[jqlValuesMsg](cmd)
	if !ok {
		t.Fatal("no value lookup")
	}
	out, _ = m.handleJQLValues(vals)
	m = out.(Model)
	if !slices.Equal(m.jql.sugg, []string{"In Progress", "Done", "currentUser()"}) {
		t.Fatalf("values = %v", m.jql.sugg)
	}
	out, _ = m.handleKey(keyMsg(t, "tab"))
	m = out.(Model)
	out, cmd = m.handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	v := m.jiraTab.views[len(m.jiraTab.views)-1]
	if m.jql != nil || cmd == nil || v.kind != jiraViewFilter || v.jql != `status = "In Progress"` {
		t.Errorf("view = %+v", v)
	}
}
