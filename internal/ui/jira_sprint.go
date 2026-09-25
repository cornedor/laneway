package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// jiraBacklogID is the sprint picker's backlog row.
const jiraBacklogID = "backlog"

// openJiraSprintPicker offers the board's open sprints and its backlog as
// where the selected card goes. A kanban board has neither.
func (m *Model) openJiraSprintPicker() {
	t := m.jiraTab
	c, ok := m.selectedJiraCard()
	if !ok {
		return
	}
	var items []jiraPickerItem
	for i, v := range t.views {
		cur := i == t.viewIdx
		switch v.kind {
		case jiraViewSprint:
			items = append(items, jiraPickerItem{id: strconv.Itoa(v.sprint), label: v.name, current: cur})
		case jiraViewBacklog:
			items = append(items, jiraPickerItem{id: jiraBacklogID, label: "Backlog", current: cur})
		}
	}
	if len(items) == 0 {
		m.status = "this board has no sprints"
		return
	}
	m.startJiraPicker(jiraPickSprint, "Move "+c.Key+" to", false)
	m.jiraPicker.issueKey = c.Key
	m.setJiraPickerItems(items)
}

// moveJiraToSprint puts key in the picked sprint or the backlog.
func (m *Model) moveJiraToSprint(key string, it jiraPickerItem) tea.Cmd {
	if it.current {
		return nil
	}
	client, ctx := m.jiraClient, m.ctx
	run := func() error { return client.MoveToBacklog(ctx, key) }
	if it.id != jiraBacklogID {
		sprint, _ := strconv.Atoi(it.id)
		run = func() error { return client.MoveToSprint(ctx, sprint, key) }
	}
	m.status = "moving " + key + " to " + it.label + "…"
	return jiraMutateCmd(key, "sprint", run)
}
