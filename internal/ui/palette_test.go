package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
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

// TestPaletteJiraSearch: after three characters the palette asks Jira, and
// hits not already listed come after its own rows.
func TestPaletteJiraSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"issues":[{"key":"ABC-3","fields":{"summary":"Third"}},{"key":"OPS-7","fields":{"summary":"Third party outage"}}]}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m = typePalette(t, m, "thi")
	seq := m.jiraPicker.fetchSeq
	out, cmd := m.handlePaletteSearch(paletteSearchMsg{seq})
	m = out.(Model)
	out, _ = m.handlePaletteFound(cmd().(paletteFoundMsg))
	m = out.(Model)
	got := paletteLabels(m)
	if len(got) != 2 || got[0] != "ABC-3  Third" || got[1] != "⌕ OPS-7  Third party outage" {
		t.Errorf("rows = %q", got)
	}
	if _, cmd := m.handlePaletteSearch(paletteSearchMsg{seq - 1}); cmd != nil {
		t.Error("a stale search should not run")
	}
}

// TestPaletteRecent: an issue opened in the panel shows in the palette as
// recent, once, newest first.
func TestPaletteRecent(t *testing.T) {
	m := loadedJiraModel(t) // opened ABC-1
	m.rememberRecent("XYZ-7", "Elsewhere")
	m.rememberRecent("ABC-1", "Fix the widget")
	if r := m.recentIssues(); len(r) != 2 || r[0][0] != "ABC-1" {
		t.Fatalf("recent = %v", r)
	}
	m.focus = focusJira
	m.openPalette()
	var got []string
	for _, it := range m.jiraPicker.all {
		if strings.HasPrefix(it.label, "recent") {
			got = append(got, it.label)
		}
	}
	if len(got) != 2 || got[1] != "recent  XYZ-7  Elsewhere" {
		t.Errorf("recent rows = %q", got)
	}
}
