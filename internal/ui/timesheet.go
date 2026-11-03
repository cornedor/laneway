package ui

import (
	"github.com/cornedor/laneway/internal/timesheet"
	"github.com/cornedor/laneway/internal/timesheet/jirasheet"
)

// WithTimesheet logs time to p instead of Jira's worklogs.
func (m Model) WithTimesheet(p timesheet.Provider) Model {
	m.sheetProvider = p
	return m
}

// sheet is where time is logged: the configured provider, else the
// current site's worklogs.
func (m *Model) sheet() timesheet.Provider {
	if m.sheetProvider != nil {
		return m.sheetProvider
	}
	return jirasheet.New(m.jiraClient)
}
