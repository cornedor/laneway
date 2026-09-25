package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestPanelFieldCursor: tab walks the fields, shift+tab steps back, esc drops
// the cursor before closing the panel, tab past the last goes to the board.
func TestPanelFieldCursor(t *testing.T) {
	m := loadedJiraModel(t)
	step := func(k string) {
		t.Helper()
		out, _ := m.handleRefKey(keyMsg(t, k))
		m = out.(Model)
	}
	step("tab")
	if got := m.panelFieldSel(); got != "Summary" {
		t.Fatalf("first tab: %q", got)
	}
	step("tab")
	step("tab")
	if got := m.panelFieldSel(); got != "Priority" {
		t.Fatalf("third tab: %q", got)
	}
	step("shift+tab")
	if got := m.panelFieldSel(); got != "Status" {
		t.Fatalf("shift+tab: %q", got)
	}
	step("esc")
	if m.panelFieldSel() != "" || !m.refOpen {
		t.Fatalf("esc: sel %q, open %v", m.panelFieldSel(), m.refOpen)
	}
	for range panelFields {
		step("tab")
	}
	if m.panelFieldSel() != "Labels" || m.focus != focusRef {
		t.Fatalf("on last: sel %q, focus %v", m.panelFieldSel(), m.focus)
	}
	step("tab")
	if m.panelFieldSel() != "" || m.focus != focusJira {
		t.Fatalf("past last: sel %q, focus %v", m.panelFieldSel(), m.focus)
	}
}

// TestPanelFieldEnter: enter opens the selected field's editor.
func TestPanelFieldEnter(t *testing.T) {
	m := loadedJiraModel(t)
	for range 4 { // Summary Status Priority Points
		out, _ := m.handleRefKey(keyMsg(t, "tab"))
		m = out.(Model)
	}
	if !strings.Contains(m.View().Content, "Points:") {
		t.Fatal("points row not drawn")
	}
	out, _ := m.handleRefKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "points" || m.jiraFieldInput.Value() != "5" {
		t.Fatalf("input: active %v, field %q, value %q", m.jiraFieldActive, m.jiraFieldName, m.jiraFieldInput.Value())
	}

	m = loadedJiraModel(t)
	out, _ = m.handleRefKey(keyMsg(t, "tab"))
	out, _ = out.(Model).handleRefKey(keyMsg(t, "tab"))
	out, cmd := out.(Model).handleRefKey(keyMsg(t, "enter"))
	if got := out.(Model); !got.jiraPicker.active || got.jiraPicker.kind != jiraPickStatus || cmd == nil {
		t.Fatalf("status picker not opened: %+v", got.jiraPicker)
	}
}

// TestPanelFieldOtherIssue: the cursor belongs to the issue it was set on.
func TestPanelFieldOtherIssue(t *testing.T) {
	m := loadedJiraModel(t)
	out, _ := m.handleRefKey(keyMsg(t, "tab"))
	m = out.(Model)
	m.fieldCursorKey = "ABC-2"
	if m.panelFieldSel() != "" {
		t.Fatal("cursor should not carry to another issue")
	}
}

// withExtra gives the loaded issue two editmeta fields, writes going to a
// fake Jira whose request bodies land in bodies.
func withExtra(t *testing.T, bodies *[]string) Model {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, r.Method+" "+r.URL.Path+" "+string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, _ := m.handlePanelExtra(panelExtraMsg{key: "ABC-1", fields: []jiraFormField{
		{FieldMeta: jira.FieldMeta{ID: "customfield_3", Name: "Team", Kind: jira.KindOption,
			Options: []jira.Option{{ID: "7", Name: "Core"}, {ID: "8", Name: "Web"}}},
			val: jira.Value{Options: []jira.Option{{ID: "7", Name: "Core"}}}},
		{FieldMeta: jira.FieldMeta{ID: "customfield_5", Name: "Ticket ref", Kind: jira.KindText}},
	}})
	return out.(Model)
}

// TestPanelExtraFields: editmeta fields show after the panel's own, the
// cursor walks onto them, and a pick writes the field.
func TestPanelExtraFields(t *testing.T) {
	var bodies []string
	m := withExtra(t, &bodies)
	view := m.View().Content
	if !strings.Contains(view, "Team:") || !strings.Contains(view, "Core") || !strings.Contains(view, "Ticket ref:") {
		t.Fatal("extra fields not drawn")
	}
	for range len(panelFields) + 1 {
		out, _ := m.handleRefKey(keyMsg(t, "tab"))
		m = out.(Model)
	}
	if got := m.panelFieldSel(); got != "Team" {
		t.Fatalf("cursor on %q", got)
	}
	out, _ := m.handleRefKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickFormOption || len(m.jiraPicker.items) != 2 {
		t.Fatalf("option picker: %+v", m.jiraPicker)
	}
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if cmd == nil || m.jiraPicker.active {
		t.Fatal("expected a write")
	}
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || msg.field != "team" {
		t.Fatalf("mutation: %+v", msg)
	}
	if len(bodies) != 1 || bodies[0] != `PUT /rest/api/3/issue/ABC-1 {"fields":{"customfield_3":{"id":"8"}}}` {
		t.Errorf("requests = %q", bodies)
	}
	if m.extraFields()[0].val.Options[0].ID != "7" {
		t.Error("shown value changed before Jira had it")
	}
}

// TestPanelExtraText: a text field opens the field input; enter writes it,
// unchanged closes without a write.
func TestPanelExtraText(t *testing.T) {
	var bodies []string
	m := withExtra(t, &bodies)
	m.fieldCursor, m.fieldCursorKey = len(panelFields)+1, "ABC-1"
	out, _ := m.handleRefKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "field" || !strings.Contains(m.View().Content, "Edit Ticket ref — ABC-1") {
		t.Fatalf("input: active %v, field %q", m.jiraFieldActive, m.jiraFieldName)
	}
	if _, cmd := m.applyJiraField(); cmd != nil {
		t.Error("unchanged value should not write")
	}
	m.jiraFieldInput.SetValue(" T-9 ")
	_, cmd := m.applyJiraField()
	if cmd == nil {
		t.Fatal("expected a write")
	}
	cmd()
	if len(bodies) != 1 || !strings.HasSuffix(bodies[0], `{"fields":{"customfield_5":"T-9"}}`) {
		t.Errorf("requests = %q", bodies)
	}
}
