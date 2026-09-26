package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFilterBuilder(t *testing.T) {
	m := jiraTabModel(t)
	press := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			out, _ := m.handleKey(keyMsg(t, k))
			m = out.(Model)
		}
	}
	// pick walks column col's cursor to the row id with the arrows.
	pick := func(col int, id string) {
		t.Helper()
		rows := m.builderRows(col)
		i := slices.IndexFunc(rows, func(it jiraPickerItem) bool { return it.id == id })
		if i < 0 {
			t.Fatalf("no %q in column %d", id, col)
		}
		for m.filterBuilder.idx[col] > i {
			press("up")
		}
		for m.filterBuilder.idx[col] < i {
			press("down")
		}
	}
	typing := func(s string) {
		t.Helper()
		for _, r := range s {
			press(string(r))
		}
	}
	press("F")
	if m.filterBuilder == nil {
		t.Fatal("F did not open the builder")
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Field", "Compare", "Value", "Status", "is not", "New · 2"} {
		if !strings.Contains(view, want) {
			t.Errorf("builder lacks %q:\n%s", want, view)
		}
	}
	// Field status, compare is, value narrowed to "prog": the term shows
	// before it is added.
	press("enter", "enter")
	typing("prog")
	if term := m.builderTerm(); term != `status:"In progress"` {
		t.Fatalf("term = %q", term)
	}
	press("enter")
	if q := m.jiraTab.search.Value(); q != `status:"In progress"` || m.filterBuilder == nil {
		t.Fatalf("query = %q, open %v", q, m.filterBuilder != nil)
	}
	// The builder stays: one more status joins the term.
	pick(2, "New")
	press("enter")
	if q := m.jiraTab.search.Value(); q != `status:"In progress",New` {
		t.Errorf("merged query = %q", q)
	}
	// Assignee is empty: no value column needed.
	press("left", "left")
	pick(0, "assignee")
	press("enter")
	pick(1, "empty")
	press("enter")
	if q := m.jiraTab.search.Value(); q != `status:"In progress",New assignee:` {
		t.Errorf("query = %q", q)
	}
	press("ctrl+x")
	if q := m.jiraTab.search.Value(); q != `status:"In progress",New` {
		t.Errorf("after ctrl+x = %q", q)
	}
	press("esc")
	if m.filterBuilder != nil {
		t.Error("esc left it open")
	}
}

// TestFilterChips: the query's terms show as chips; a click on one removes
// it.
func TestFilterChips(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.search.SetValue(`status:new points>=5 "first one"`)
	m.applyJiraSearch()
	row := ansi.Strip(strings.Split(m.View().Content, "\n")[jiraBodyTop-1])
	x := strings.Index(row, "points>=5 ×")
	if x < 0 {
		t.Fatalf("no chip in %q", row)
	}
	m, _ = clickAt(m, x+2, jiraBodyTop-1)
	if q := m.jiraTab.search.Value(); q != `status:new "first one"` {
		t.Fatalf("after click: %q", q)
	}
	m.removeSearchTerm(1)
	m.removeSearchTerm(0)
	if m.jiraTab.jiraSearchQuery() != "" {
		t.Errorf("query left: %q", m.jiraTab.search.Value())
	}
}
