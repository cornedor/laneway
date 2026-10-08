package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// V lists the board project's versions with how much of each is done.
// Enter on one shows its issues as a view; the row under an unreleased one
// releases it today, on a second enter.

// releasePrefix marks a release row's id; the rest is the version's.
const releasePrefix = "release:"

// openReleases loads the project's versions into a picker.
func (m *Model) openReleases() tea.Cmd {
	project := m.jiraTab.project
	if project == "" {
		m.status = i18n.T("open a board first")
		return nil
	}
	gen := m.startJiraPicker(jiraPickReleases, i18n.Tf("Releases — %s", project), true)
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		vs, err := c.Versions(ctx, project)
		items := releaseItems(vs)
		if err == nil && len(items) == 0 {
			items = []jiraPickerItem{{label: i18n.Tf("no versions in %s", project)}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickReleases, items: items, err: err}
	}
}

// releaseItems are the unarchived versions, each "1.2  ████░░░░ 4/8 done",
// an unreleased one followed by its release row.
func releaseItems(vs []jira.Version) []jiraPickerItem {
	var items []jiraPickerItem
	for _, v := range vs {
		if v.Archived {
			continue
		}
		when := i18n.T("unreleased")
		if v.Released {
			when = i18n.Tf("released %s", v.ReleaseDate)
		} else if v.ReleaseDate != "" {
			when = i18n.Tf("due %s", v.ReleaseDate)
		}
		items = append(items, jiraPickerItem{id: v.ID, value: v.Name,
			label: i18n.Tf("%s  %s %d/%d done  · %s", v.Name, releaseBar(v.Done, v.Total), v.Done, v.Total, when)})
		if !v.Released {
			label := i18n.Tf("  ↳ release %s today", v.Name)
			if open := v.Total - v.Done; open > 0 {
				label += i18n.Tf(" (%d not done)", open)
			}
			items = append(items, jiraPickerItem{id: releasePrefix + v.ID, value: v.Name, label: label, search: v.Name})
		}
	}
	return items
}

// releaseBar is done of total as eight cells.
func releaseBar(done, total int) string {
	const cells = 8
	n := 0
	if total > 0 {
		n = done * cells / total
	}
	return strings.Repeat("█", n) + strings.Repeat("░", cells-n)
}

// applyRelease acts on the picked row: a version's issues as a view, or
// its release once confirmed.
func (m Model) applyRelease(it jiraPickerItem) (tea.Model, tea.Cmd) {
	id, release := strings.CutPrefix(it.id, releasePrefix)
	if id == "" {
		return m, nil
	}
	if !release {
		m.closeJiraPicker()
		return m, m.runNamedJQLView(i18n.Tf("Release: %s", it.value), "fixVersion = "+id+" ORDER BY status, rank")
	}
	if m.jiraPicker.pendingDelete != it.id { // confirmed by a second enter on it
		m.jiraPicker.pendingDelete = it.id
		m.status = i18n.Tf("enter again releases %s", it.value)
		return m, nil
	}
	m.closeJiraPicker()
	c, ctx, name := m.jiraClient, m.ctx, it.value
	m.status = i18n.Tf("releasing %s…", name)
	return m, jiraMutateCmd(name, "release", func() error { return c.ReleaseVersion(ctx, id, time.Now()) })
}
