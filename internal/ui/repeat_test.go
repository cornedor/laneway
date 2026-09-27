package ui

import (
	"strings"
	"testing"
)

// TestRepeat: . makes the last quick edit again on the selected card.
func TestRepeat(t *testing.T) {
	m := jiraTabModel(t)
	writes := undoJira(t, &m)
	if _, cmd := m.handleJiraKey(keyMsg(t, ".")); cmd != nil {
		t.Fatal("nothing to repeat at first")
	}
	m.quickKey = "ABC-1"
	m.applyBulkPick(jiraPickPriority, []string{"ABC-1"}, jiraPickerItem{id: "2", label: "priority High"})
	m.quickKey = ""
	m.selectJiraKey("ABC-3")
	out, cmd := m.handleJiraKey(keyMsg(t, "."))
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("no repeat: %q", m.status)
	}
	msg := cmd().(bulkDoneMsg)
	if len(msg.keys) != 1 || msg.keys[0] != "ABC-3" || !msg.quick {
		t.Fatalf("%+v", msg)
	}
	if last := (*writes)[len(*writes)-1]; !strings.Contains(last, `/issue/ABC-3 {"fields":{"priority":{"id":"2"}}}`) {
		t.Errorf("wrote %q", last)
	}
}
