package ui

import (
	"charm.land/lipgloss/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
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

// TestPalettePinned: * in the panel pins its issue, listed first in the
// palette and only once; * again unpins it.
func TestPalettePinned(t *testing.T) {
	m := loadedJiraModel(t) // opened ABC-1
	m.focus = focusRef
	out, _ := m.handleKey(keyMsg(t, "*"))
	m = out.(Model)
	if p := m.pinnedIssues(); len(p) != 1 || p[0][0] != "ABC-1" || !strings.HasPrefix(m.status, "pinned ABC-1") {
		t.Fatalf("pinned = %v, status %q", p, m.status)
	}
	m.rememberRecent("ABC-1", "Fix the widget")
	m.focus = focusJira
	m.openPalette()
	n := 0
	for _, it := range m.jiraPicker.all {
		if it.id == "i:ABC-1" {
			n++
		}
	}
	if m.jiraPicker.all[0].label != "pinned  ABC-1  "+m.jiraIssue.Summary || n != 1 {
		t.Errorf("first row %q, ABC-1 rows %d", m.jiraPicker.all[0].label, n)
	}
	m.closeJiraPicker()
	m.focus = focusRef
	out, _ = m.handleKey(keyMsg(t, "*"))
	m = out.(Model)
	if p := m.pinnedIssues(); len(p) != 0 || m.status != "unpinned ABC-1" {
		t.Errorf("after unpin = %v, status %q", p, m.status)
	}
}

// TestPickerKeepsItsSize: a searchable picker is as big with one row, none
// or a long one as with many, so async results don't resize it.
func TestPickerKeepsItsSize(t *testing.T) {
	m := jiraTabModel(t)
	m.openPalette()
	size := func() (int, int) {
		out := m.renderJiraPicker(m.bodyH())
		return lipgloss.Width(out), lipgloss.Height(out)
	}
	w, h := size()
	for _, items := range [][]jiraPickerItem{
		nil,
		{{id: "x", label: strings.Repeat("a very long label ", 20)}},
	} {
		m.setJiraPickerItems(items)
		if w2, h2 := size(); w2 != w || h2 != h {
			t.Errorf("%d rows: %d×%d, want %d×%d", len(items), w2, h2, w, h)
		}
	}
	m.jiraPicker.loading = true
	if w2, h2 := size(); w2 != w || h2 != h {
		t.Errorf("loading: %d×%d, want %d×%d", w2, h2, w, h)
	}
}

// TestPaletteNamedFilter: a ui.filters query is a palette row that sets
// the / search.
func TestPaletteNamedFilter(t *testing.T) {
	m := jiraTabModel(t)
	o, warn := optionsFrom(config.UIConfig{Filters: []config.NamedQuery{{Name: "New ones", Query: "status:new"}, {Name: "x"}}})
	if len(o.filters) != 1 || len(warn) != 1 {
		t.Fatalf("filters %v %v", o.filters, warn)
	}
	m.opts.filters = o.filters
	m.openPalette()
	i := slices.IndexFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == "s:0" })
	if i < 0 || !strings.Contains(m.jiraPicker.items[i].label, "New ones") {
		t.Fatalf("no row: %+v", m.jiraPicker.items)
	}
	m.jiraPicker.idx = i
	out, _ := m.applyJiraPick()
	if m = out.(Model); m.jiraTab.search.Value() != "status:new" || len(m.jiraTab.order) != 2 {
		t.Errorf("query %q, %d shown", m.jiraTab.search.Value(), len(m.jiraTab.order))
	}
}
