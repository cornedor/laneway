package ui

import "testing"

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
