package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// A → Dependency tree: what holds the issue up, through their own
// blockers, and what it holds up; enter opens a row's issue.

func (m *Model) openDependencies(key string) tea.Cmd {
	gen := m.startJiraPicker(jiraPickDeps, i18n.Tf("Dependencies — %s", key), true)
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		root, by, blocks, err := c.Dependencies(ctx, key)
		items := []jiraPickerItem{{id: root.Key, label: depLabel(root)}}
		section := func(title string, nodes []jira.DepNode) {
			if len(nodes) == 0 {
				items = append(items, jiraPickerItem{label: i18n.Tf("  %s: nothing", title)})
				return
			}
			items = append(items, jiraPickerItem{label: "  " + title})
			var walk func(ns []jira.DepNode, indent string)
			walk = func(ns []jira.DepNode, indent string) {
				for i, n := range ns {
					branch, next := "├ ", "│ "
					if i == len(ns)-1 {
						branch, next = "└ ", "  "
					}
					items = append(items, jiraPickerItem{id: n.Key, label: indent + branch + depLabel(n)})
					walk(n.Kids, indent+next)
				}
			}
			walk(nodes, "    ")
		}
		section(i18n.T("held up by"), by)
		section(i18n.T("holds up"), blocks)
		open := 0
		var count func(ns []jira.DepNode)
		count = func(ns []jira.DepNode) {
			for _, n := range ns {
				if !n.Done && !n.Seen {
					open++
				}
				count(n.Kids)
			}
		}
		count(by)
		title := i18n.Tf("Dependencies — %s", key)
		if open > 0 {
			title += " · " + i18n.Tf("held up by %s", i18n.Tn(open, "%d open issue", "%d open issues", open))
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickDeps, items: items, err: err, title: title}
	}
}

// depLabel is "ABC-2 Fix login [In Progress]", done ones ✓, one already
// above ↺.
func depLabel(n jira.DepNode) string {
	s := n.Key + " " + n.Summary + " [" + n.Status + "]"
	switch {
	case n.Seen:
		s += " ↺"
	case n.Done:
		s += " ✓"
	}
	return s
}

// plural is "1 open issue", "3 open issues".
func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}
