package ui

import (
	"strings"
	"testing"
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
