package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// D in the panel: the pull requests (open first) and branches linked to the
// issue; enter opens one in the browser.

func (m *Model) openDevInfo() tea.Cmd {
	if m.jiraIssue == nil {
		return nil
	}
	key := m.jiraIssue.Key
	gen := m.startJiraPicker(jiraPickDev, "Development — "+key, true)
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		items, err := c.DevInfo(ctx, key)
		rows := make([]jiraPickerItem, len(items))
		for i, d := range items {
			label := fmt.Sprintf("branch  %s", d.Name)
			switch d.Kind {
			case "pr":
				label = fmt.Sprintf("%-8s %s  (%s)", d.Status, d.Name, d.Branch)
			case "commit":
				label = fmt.Sprintf("commit  %s  — %s", d.Name, d.Status)
			}
			if d.Repo != "" {
				label += "  " + d.Repo
			}
			rows[i] = jiraPickerItem{id: d.URL, label: label}
		}
		if err == nil && len(rows) == 0 {
			rows = []jiraPickerItem{{label: "no branches or pull requests linked"}}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickDev, items: rows, err: err}
	}
}
