package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestFormFieldKeys: tab, ↓ and ctrl+n leave a line being edited for the
// next field, keeping it; shift+tab, ↑ and ctrl+p for the one before.
func TestFormFieldKeys(t *testing.T) {
	for _, tc := range []struct {
		key string
		d   int
	}{{"tab", 1}, {"down", 1}, {"ctrl+n", 1}, {"shift+tab", -1}, {"up", -1}, {"ctrl+p", -1}} {
		m := jiraTabModel(t)
		out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
		m = out.(Model)
		f := m.jiraForm
		start := f.idx
		if !f.editing || f.fields[start].ID != createSummaryField {
			t.Fatal("the form opens on the summary")
		}
		out, _ = m.handleJiraFormKey(keyStr("x"))
		m = out.(Model)
		out, _ = m.handleJiraFormKey(keyMsg(t, tc.key))
		m = out.(Model)
		if f.editing || f.fields[start].val.Text != "x" {
			t.Errorf("%s: editing %v, summary %q", tc.key, f.editing, f.fields[start].val.Text)
		}
		if want := min(max(start+tc.d, 0), len(f.fields)); f.idx != want {
			t.Errorf("%s: field %d, want %d", tc.key, f.idx, want)
		}
	}
}

// TestFormMultilineLeaves: in the description ↑ and ↓ move lines until the
// edge, then the field; tab leaves at once.
func TestFormMultilineLeaves(t *testing.T) {
	open := func() (Model, *jiraFormState, int) {
		m := jiraTabModel(t)
		out, _ := m.handleJiraCreateTypes(jiraCreateTypesMsg{project: "ABC", types: []jira.Option{{Name: "Task"}}})
		m = out.(Model)
		f := m.jiraForm
		f.editing = false
		f.idx = slices.IndexFunc(f.fields, func(ff jiraFormField) bool { return ff.ID == createDescField })
		f.fields[f.idx].val.Text = "one\ntwo"
		m.editJiraFormField()
		m.View() // sizes the editor
		return m, f, f.idx
	}
	press := func(m Model, k string) Model {
		out, _ := m.handleJiraFormKey(keyMsg(t, k))
		return out.(Model)
	}

	m, f, desc := open() // the caret starts on the last line
	m = press(m, "up")
	if !f.editing || f.idx != desc {
		t.Fatal("↑ on the second line left the field")
	}
	press(m, "up")
	if f.editing || f.idx != desc-1 || f.fields[desc].val.Text != "one\ntwo" {
		t.Fatalf("↑ on the top line: editing %v, field %d", f.editing, f.idx)
	}

	m, f, desc = open()
	press(m, "down")
	if f.editing || f.idx != desc+1 {
		t.Fatalf("↓ on the bottom line: editing %v, field %d", f.editing, f.idx)
	}

	m, f, desc = open()
	m = press(m, "up")
	press(m, "tab")
	if f.editing || f.idx != desc+1 {
		t.Fatalf("tab: editing %v, field %d", f.editing, f.idx)
	}
}

// TestSettingsFieldKeys: tab moves through settings, and out of an edit it
// saves the value first.
func TestSettingsFieldKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  stale_days: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := jiraTabModel(t).WithConfigPath(path)
	m.uiConfig.StaleDays = 5
	press := func(k string) {
		t.Helper()
		out, _ := m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	press(",")
	press("tab")
	if m.settings.idx != 1 {
		t.Fatalf("tab: row %d", m.settings.idx)
	}
	press("shift+tab")
	if m.settings.idx != 0 {
		t.Fatalf("shift+tab: row %d", m.settings.idx)
	}
	m.settings.idx = slices.IndexFunc(m.settings.rows, func(r settingRow) bool { return r.name == "stale_days" })
	at := m.settings.idx
	press("enter")
	m.settings.input.SetValue("0x")
	press("tab")
	if m.settings.input == nil || m.settings.idx != at {
		t.Fatal("tab left a bad value")
	}
	m.settings.input.SetValue("3")
	press("down")
	if m.settings.input != nil || m.settings.idx != at+1 || m.opts.staleDays != 3 {
		t.Fatalf("↓: input %v, row %d, stale %d", m.settings.input != nil, m.settings.idx, m.opts.staleDays)
	}
}
