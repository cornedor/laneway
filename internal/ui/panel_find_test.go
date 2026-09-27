package ui

import (
	"strings"
	"testing"
)

// TestPanelFind: / finds text in the panel, n steps on and wraps round, a
// miss says so.
func TestPanelFind(t *testing.T) {
	m := loadedJiraModel(t)
	m.jiraIssue.Description = strings.Repeat("filler\n\n", 40) + "the widget broke\n\n" + strings.Repeat("more\n\n", 40)
	m.renderRef()
	out, _ := m.handleRefKey(keyMsg(t, "/"))
	if m = out.(Model); !m.jiraFieldActive || m.jiraFieldName != "find" {
		t.Fatal("/ should ask what to find")
	}
	m.jiraFieldInput.SetValue("WIDGET")
	out, _ = m.applyJiraField()
	m = out.(Model)
	first := m.panelFindAt
	if !strings.Contains(m.status, "1 of 2") || !strings.Contains(strings.ToLower(m.panelPlain[first]), "widget") {
		t.Fatalf("status %q, hit %q", m.status, m.panelPlain[first])
	}
	out, _ = m.handleRefKey(keyMsg(t, "n"))
	m = out.(Model)
	if !strings.Contains(m.status, "2 of 2") || m.panelFindAt <= first || m.refView.YOffset() == 0 {
		t.Errorf("n: status %q, at %d, top %d", m.status, m.panelFindAt, m.refView.YOffset())
	}
	out, _ = m.handleRefKey(keyMsg(t, "n"))
	if m = out.(Model); m.panelFindAt != first {
		t.Error("n past the last should wrap to the first")
	}
	m.panelFind = "nowhere"
	m.findInPanel(1)
	if !strings.Contains(m.status, `no "nowhere"`) {
		t.Errorf("miss: %q", m.status)
	}
}
