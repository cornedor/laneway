package ui

import tea "charm.land/bubbletea/v2"

// @ switches between the config's Jira sites. The app ends with the pick
// and main starts it again on that site, with that site's own state.

// WithSites tells the model the configured sites ("" is jira:) and which
// one it shows.
func (m Model) WithSites(names []string, current string) Model {
	m.sites, m.site = names, current
	return m
}

// NextSite is the site picked to switch to, when the app ended for that.
func (m Model) NextSite() (string, bool) {
	if m.nextSite == nil {
		return "", false
	}
	return *m.nextSite, true
}

// openSitePicker lists the sites, the current one ticked.
func (m *Model) openSitePicker() {
	if len(m.sites) < 2 {
		m.status = "one Jira site configured; add more under sites:"
		return
	}
	m.startJiraPicker(jiraPickSite, "Jira site", false)
	items := make([]jiraPickerItem, len(m.sites))
	for i, s := range m.sites {
		items[i] = jiraPickerItem{id: s, label: siteLabel(s), current: s == m.site}
	}
	m.setJiraPickerItems(items)
}

// pickSite ends the app to start again on site.
func (m Model) pickSite(site string) (tea.Model, tea.Cmd) {
	if site == m.site {
		return m, nil
	}
	m.nextSite = &site
	return m, tea.Quit
}

func siteLabel(s string) string {
	if s == "" {
		return "default (jira:)"
	}
	return s
}
