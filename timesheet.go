package main

import (
	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/timesheet"
	"github.com/cornedor/laneway/internal/timesheet/httpsheet"
)

// timesheetProvider is the timesheet: section's http provider, nil for
// Jira's worklogs on the site. A section laneway can't use warns and
// leaves time on Jira.
func timesheetProvider(cfg config.Config) (timesheet.Provider, []string) {
	isHTTP, err := cfg.Timesheet.IsHTTP()
	if err != nil {
		return nil, []string{err.Error() + "; time goes to Jira's worklogs"}
	}
	if !isHTTP {
		return nil, nil
	}
	t, err := cfg.Timesheet.WithToken()
	if err != nil {
		return nil, []string{err.Error() + "; time goes to Jira's worklogs"}
	}
	return httpsheet.New(t.URL, t.Token), nil
}
