package ui

import "testing"

// TestSiteSwitch: @ lists the sites and a row adding one; picking another
// ends the app with it.
func TestSiteSwitch(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "@"))
	m = out.(Model)
	if !m.jiraPicker.active || len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].id != addSiteID {
		t.Fatalf("one site: only the add row, got %+v", m.jiraPicker.items)
	}
	m.closeJiraPicker()
	m = m.WithSites([]string{"", "work"}, "")
	out, _ = m.handleJiraKey(keyMsg(t, "@"))
	m = out.(Model)
	if !m.jiraPicker.active || len(m.jiraPicker.items) != 3 || !m.jiraPicker.items[0].current {
		t.Fatalf("picker = %+v", m.jiraPicker)
	}
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if next, ok := m.NextSite(); !ok || next != "work" || cmd == nil {
		t.Errorf("next %q %v", next, ok)
	}
}

// TestAddSite: the add row ends the app to add a site, not to switch.
func TestAddSite(t *testing.T) {
	m := jiraTabModel(t).WithSites([]string{"", "work"}, "")
	out, _ := m.handleJiraKey(keyMsg(t, "@"))
	m = out.(Model)
	m.jiraPicker.idx = len(m.jiraPicker.items) - 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if _, switched := m.NextSite(); !m.AddSite() || switched || cmd == nil {
		t.Errorf("add %v switched %v cmd %v", m.AddSite(), switched, cmd != nil)
	}
}
