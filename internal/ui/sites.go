package ui

import tea "charm.land/bubbletea/v2"

// @ switches between the config's Jira sites. The app ends with the pick
// and main starts it again on that site, with that site's own state.

// WithSites tells the model the configured sites ("" is jira:) and which
// one it shows.
func (m Model) WithSites(names []string, current, defaultName string) Model {
	m.sites, m.site, m.defaultSiteName = names, current, defaultName
	return m
}

// AddSite is whether the app ended to set up another site.
func (m Model) AddSite() bool { return m.addSite }

// addSiteID is the picker row that adds a site; no site name has a space.
const addSiteID = "+ add"

// NextSite is the site picked to switch to, when the app ended for that.
func (m Model) NextSite() (string, bool) {
	if m.nextSite == nil {
		return "", false
	}
	return *m.nextSite, true
}

// openSitePicker lists the sites, the current one ticked, and a last row
// that adds one.
func (m *Model) openSitePicker() {
	m.startJiraPicker(jiraPickSite, "Jira site", false)
	items := make([]jiraPickerItem, 0, len(m.sites)+1)
	for _, s := range m.sites {
		items = append(items, jiraPickerItem{id: s, label: m.siteLabel(s), current: s == m.site})
	}
	items = append(items, jiraPickerItem{id: addSiteID, label: "+ add a Jira site", focus: len(m.sites) < 2})
	m.setJiraPickerItems(items)
}

// pickSite ends the app to start again on site, or to add one: main asks
// for it in the terminal, then starts on it.
func (m Model) pickSite(site string) (tea.Model, tea.Cmd) {
	if site == m.site {
		return m, nil
	}
	if w := m.unsentWork(); w != "" && !m.quitAsked {
		m.quitAsked = true // the next pick (or quit) goes
		m.status = w + " · pick it again to switch anyway"
		return m, nil
	}
	if site == addSiteID {
		m.addSite = true
	} else {
		m.nextSite = &site
	}
	return m, tea.Quit
}

func (m Model) siteLabel(s string) string {
	if s != "" {
		return s
	}
	if m.defaultSiteName != "" {
		return m.defaultSiteName
	}
	return "default (jira:)"
}
