package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFilterBuilder(t *testing.T) {
	m := jiraTabModel(t)
	pick := func(id string) {
		t.Helper()
		for i, it := range m.jiraPicker.items {
			if it.id == id {
				m.jiraPicker.idx = i
				out, _ := m.applyJiraPick()
				m = out.(Model)
				return
			}
		}
		t.Fatalf("no row %q in %+v", id, m.jiraPicker.items)
	}
	out, _ := m.handleKey(keyMsg(t, "F"))
	m = out.(Model)
	if m.jiraPicker.kind != jiraPickFilterField {
		t.Fatal("F did not open the builder")
	}
	pick("status")
	pick(":")
	if first := m.jiraPicker.items[0]; first.id != "New" || first.label != "New · 2" {
		t.Errorf("most common status first with its count: %+v", first)
	}
	pick("In progress")
	if q := m.jiraTab.search.Value(); q != `status:"In progress"` {
		t.Errorf("query = %q", q)
	}
	for _, step := range []string{"F", "status", ":", "New"} {
		if step == "F" {
			out, _ = m.handleKey(keyMsg(t, "F"))
			m = out.(Model)
			continue
		}
		pick(step)
	}
	if q := m.jiraTab.search.Value(); q != `status:"In progress",New` {
		t.Errorf("merged query = %q", q)
	}
	if n := len(m.jiraTab.lanes[0].cards) + len(m.jiraTab.lanes[1].cards) + len(m.jiraTab.lanes[2].cards); n != 3 {
		t.Errorf("%d cards shown, want 3", n)
	}
	m.openFilterBuilder()
	pick("points")
	pick(">=")
	pick("5")
	m.openFilterBuilder()
	pick("assignee")
	pick("empty")
	if q := m.jiraTab.search.Value(); q != `status:"In progress",New points>=5 assignee:` {
		t.Errorf("query = %q", q)
	}
}

// TestFilterChips: the query's terms show as chips; a click on one, or its
// row in the builder, removes it.
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
	m.openFilterBuilder()
	if it := m.jiraPicker.items[1]; it.label != `× "first one"` {
		t.Fatalf("builder row = %+v", it)
	}
	m.jiraPicker.idx = 0
	out, _ := m.applyJiraPick()
	m = out.(Model)
	m.openFilterBuilder()
	m.jiraPicker.idx = 0
	out, _ = m.applyJiraPick()
	if m = out.(Model); m.jiraTab.jiraSearchQuery() != "" {
		t.Errorf("query left: %q", m.jiraTab.search.Value())
	}
}
