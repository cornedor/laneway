package ui

import (
	"strings"
	"testing"
)

// typePalette opens the palette and types q into its filter.
func typePalette(t *testing.T, m Model, q string) Model {
	t.Helper()
	out, _ := m.handleKey(keyMsg(t, ":"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickPalette {
		t.Fatal(": should open the palette")
	}
	for _, r := range q {
		out, _ = m.handleKey(keyMsg(t, string(r)))
		if r == ' ' {
			out, _ = m.handleKey(keyMsg(t, "space"))
		}
		m = out.(Model)
	}
	return m
}

func paletteLabels(m Model) []string {
	var out []string
	for _, it := range m.jiraPicker.items {
		out = append(out, it.label)
	}
	return out
}

// TestPaletteRows: actions, views, quick filters, boards and issues; every
// word of the filter must match.
func TestPaletteRows(t *testing.T) {
	m := typePalette(t, jiraTabModel(t), "")
	all := strings.Join(paletteLabels(m), "\n")
	for _, want := range []string{"roadmap  R", "view  Backlog", "filter  FE", "board  ABC board", "ABC-3  Third"} {
		if !strings.Contains(all, want) {
			t.Errorf("palette lacks %q", want)
		}
	}
	if strings.Contains(all, "page down") {
		t.Error("cursor moves should not be listed")
	}
	m = typePalette(t, jiraTabModel(t), "abc thi")
	if got := paletteLabels(m); len(got) != 1 || got[0] != "ABC-3  Third" {
		t.Errorf("filtered = %q", got)
	}
}

// TestPaletteRun: an action runs as its key, a view switches, an issue opens.
func TestPaletteRun(t *testing.T) {
	m := typePalette(t, jiraTabModel(t), "roadmap")
	out, _ := m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); m.jiraTab.roadmap == nil || m.jiraPicker.active {
		t.Fatal("roadmap action should open the roadmap")
	}

	m = typePalette(t, jiraTabModel(t), "view backlog")
	seq := m.jiraTab.seq
	out, cmd := m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); cmd == nil || m.jiraTab.seq != seq+1 {
		t.Error("view row should load that view")
	}

	m = typePalette(t, jiraTabModel(t), "ABC-2")
	out, cmd = m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); !m.refOpen || cmd == nil || m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Error("issue row should open it in the panel")
	}
}

// TestPalettePanel: from the panel the palette lists the panel's actions.
func TestPalettePanel(t *testing.T) {
	m := typePalette(t, loadedJiraModel(t), "labels")
	out, _ := m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); !m.jiraFieldActive || m.jiraFieldName != "labels" {
		t.Errorf("edit labels should open the labels input: %q", m.jiraFieldName)
	}
}

func TestKeyPress(t *testing.T) {
	for _, s := range []string{"R", "enter", "ctrl+u", "shift+left", "space", "#", "pgdown"} {
		if got := keyPress(s).String(); got != s {
			t.Errorf("keyPress(%q) = %q", s, got)
		}
	}
}
