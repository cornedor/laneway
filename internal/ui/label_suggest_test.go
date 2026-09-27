package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

func TestLabelWord(t *testing.T) {
	for in, want := range map[string][3]string{
		"ui ap":  {"", "ap", ""},
		"ui -ol": {"-", "ol", ""},
		"":       {"", "", ""},
		"ui ":    {"", "", ""},
	} {
		ti := textinput.New()
		ti.SetValue(in)
		ti.CursorEnd()
		if sign, word, _ := labelWord(&ti); sign != want[0] || word != want[1] {
			t.Errorf("%q = %q %q", in, sign, word)
		}
	}
}

// TestLabelSuggest: typing in the panel's labels lists Jira's matching
// labels under it, those set left out; ↓ and tab take one.
func TestLabelSuggest(t *testing.T) {
	m, _ := actionsModel(t, map[string]string{
		"/rest/api/3/jql/autocompletedata/suggestions": `{"results":[{"value":"ui"},{"value":"uikit"},{"value":"uiux"}]}`,
	})
	m.jiraIssue.Labels = []string{"ui"}
	out, _ := m.handleRefKey(keyMsg(t, "l"))
	m = out.(Model)
	for _, k := range []string{"space", "u"} {
		out, _ = m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	out, cmd := m.handleLabelTick(labelTickMsg{m.labels.seq})
	m = out.(Model)
	out, _ = m.handleLabelsFound(cmd().(labelsFoundMsg))
	m = out.(Model)
	if got := m.labels.list; len(got) != 2 || got[0] != "uikit" {
		t.Fatalf("suggestions = %q", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "▸ uikit") {
		t.Error("the panel should list them")
	}
	out, _ = m.handleKey(keyMsg(t, "down"))
	out, _ = out.(Model).handleKey(keyMsg(t, "tab"))
	m = out.(Model)
	if got := m.jiraFieldInput.Value(); got != "ui uiux " || len(m.labels.list) != 0 {
		t.Errorf("value %q, list %q", got, m.labels.list)
	}
}

// TestCustomLabelsSuggest: a custom labels field in the create form
// completes from its own clause.
func TestCustomLabelsSuggest(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("fieldName")
		io.WriteString(w, `{"results":[{"value":"team-a"}]}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.createMore = true
	out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
	m = out.(Model)
	f := m.jiraForm
	f.editing = false
	out, _ = m.handleCreateFields(createFieldsMsg{project: "ABC", typ: "Task", seq: f.create.fieldsSeq, fields: []jira.CreateField{
		{FieldMeta: jira.FieldMeta{ID: "customfield_10050", Name: "Team", Kind: jira.KindStrings, Clause: "cf[10050]"}},
	}})
	m = out.(Model)
	f.idx = len(f.fields) - 1
	m.editJiraFormField()
	out, _ = m.handleJiraFormKey(keyStr("t"))
	m = out.(Model)
	out, cmd := m.handleLabelTick(labelTickMsg{m.labels.seq})
	m = out.(Model)
	out, _ = m.handleLabelsFound(cmd().(labelsFoundMsg))
	m = out.(Model)
	if asked != "cf[10050]" || len(m.labels.list) != 1 || !strings.Contains(ansi.Strip(m.View().Content), "▸ team-a") {
		t.Errorf("asked %q, list %q", asked, m.labels.list)
	}
}
