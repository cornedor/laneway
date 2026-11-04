package ui

import (
	"strings"
	"testing"
)

// TestPanelFind: ctrl+f finds text in the panel, n steps on and wraps round, a
// miss says so.
func TestPanelFind(t *testing.T) {
	m := loadedJiraModel(t)
	m.jiraIssue.Description = strings.Repeat("filler\n\n", 40) + "the widget broke\n\n" + strings.Repeat("more\n\n", 40)
	m.renderRef()
	out, _ := m.handleRefKey(keyMsg(t, "ctrl+f"))
	if m = out.(Model); !m.jiraFieldActive || m.jiraFieldName != "find" {
		t.Fatal("ctrl+f should ask what to find")
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

// TestPanelSlashSearchesBoard: / in the panel is the board's search, not
// the find in the issue.
func TestPanelSlashSearchesBoard(t *testing.T) {
	m := loadedJiraModel(t)
	m.focus = focusRef
	out, _ := m.handleKey(keyMsg(t, "/"))
	m = out.(Model)
	if m.jiraFieldActive || !m.jiraTab.searching || m.focus != focusJira {
		t.Fatalf("field %v, searching %v, focus %v", m.jiraFieldActive, m.jiraTab.searching, m.focus)
	}
}

// TestPanelFindWrapped: a phrase the panel's wrap splits over two rows is
// still found, on the row it starts.
func TestPanelFindWrapped(t *testing.T) {
	m := loadedJiraModel(t)
	m.jiraIssue.Description = strings.Repeat("filler words ", 30) + "needle haystack"
	m.renderRef()
	// Find a break and search the two words either side of it.
	for i, l := range m.panelPlain {
		if i+1 >= len(m.panelPlain) || !m.panelSoft[i] {
			continue
		}
		f := strings.Fields(l)
		q := f[len(f)-1] + " " + strings.Fields(m.panelPlain[i+1])[0]
		m.panelFind = q
		m.panelFindAt = i - 1
		m.findInPanel(1)
		if m.panelFindAt != i {
			t.Fatalf("%q over the break at row %d: found at %d (%s)", q, i, m.panelFindAt, m.status)
		}
		return
	}
	t.Fatalf("no wrapped row:\n%s", strings.Join(m.panelPlain, "\n"))
}
