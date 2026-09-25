package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A in the panel: what else can be done with the issue — a subtask (or, on
// an epic, a child), a link to another issue, a clone, watching it.

// jiraWatchMsg is a watch toggled.
type jiraWatchMsg struct {
	key string
	on  bool
	err error
}

// openIssueActions lists the actions for the panel issue.
func (m *Model) openIssueActions() {
	if m.jiraIssue == nil {
		return
	}
	iss := m.jiraIssue
	m.startJiraPicker(jiraPickIssueActions, iss.Key, false)
	items := []jiraPickerItem{{id: "subtask", label: "New subtask"}}
	if strings.EqualFold(iss.Type, "epic") {
		items = []jiraPickerItem{{id: "child", label: "New issue in this epic"}}
	}
	items = append(items,
		jiraPickerItem{id: "link", label: "Link to another issue"},
		jiraPickerItem{id: "clone", label: "Clone"},
		jiraPickerItem{id: "watch", label: "Watch / stop watching"},
	)
	m.setJiraPickerItems(items)
}

// applyIssueAction runs the picked action on key.
func (m *Model) applyIssueAction(key, id string) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	switch id {
	case "subtask", "child":
		project := issueProject(key)
		gen := m.startJiraPicker(jiraPickCreateType, "New "+id+" of "+key, false)
		m.jiraCreateParent, m.jiraCreateProject = key, project
		seq := m.jiraPicker.fetchSeq
		return func() tea.Msg {
			types, err := c.IssueTypes(ctx, project)
			if id == "subtask" {
				types, err = c.SubtaskTypes(ctx, project)
			}
			items := make([]jiraPickerItem, 0, len(types))
			for _, t := range types {
				if !strings.EqualFold(t.Name, "epic") {
					items = append(items, jiraPickerItem{id: t.Name, label: jiraTypeIcon(t.Name) + " " + t.Name})
				}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickCreateType, items: items, err: err}
		}
	case "link":
		gen := m.startJiraPicker(jiraPickLinkType, "Link "+key, false)
		m.jiraPicker.issueKey = key
		seq := m.jiraPicker.fetchSeq
		return func() tea.Msg {
			types, err := c.LinkTypes(ctx)
			var items []jiraPickerItem
			for _, t := range types {
				items = append(items, jiraPickerItem{id: "out|" + t.Name, label: key + " " + t.Outward + " …"})
				if t.Inward != t.Outward {
					items = append(items, jiraPickerItem{id: "in|" + t.Name, label: key + " " + t.Inward + " …"})
				}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickLinkType, items: items, err: err}
		}
	case "clone":
		m.status = "cloning " + key + "…"
		return func() tea.Msg {
			nk, err := c.Clone(ctx, key)
			return jiraCreatedMsg{key: nk, err: err}
		}
	case "watch":
		return func() tea.Msg {
			on, err := c.ToggleWatch(ctx, key)
			return jiraWatchMsg{key: key, on: on, err: err}
		}
	}
	return nil
}

// openLinkTarget asks which issue the picked link goes to.
func (m *Model) openLinkTarget(key string, it jiraPickerItem) {
	m.openBulkInput("link", "issue key, or a number in "+issueProject(key))
	m.jiraFieldKey = key
	m.jiraLinkChoice = it
}

// applyLink links the panel issue and the typed key.
func (m Model) applyLink(raw string) (tea.Model, tea.Cmd) {
	key := m.jiraFieldKey
	target := jiraGotoKey(raw, issueProject(key))
	if target == "" || target == key {
		m.status = "not an issue key: " + raw
		return m, nil
	}
	dir, typ, _ := strings.Cut(m.jiraLinkChoice.id, "|")
	out, in := key, target
	if dir == "in" {
		out, in = target, key
	}
	m.closeJiraField()
	c, ctx := m.jiraClient, m.ctx
	m.status = "linking " + key + " and " + target + "…"
	return m, jiraMutateCmd(key, "links", func() error { return c.LinkIssues(ctx, typ, out, in) })
}

func (m Model) handleJiraWatch(msg jiraWatchMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.status = msg.key + " watch: " + msg.err.Error()
	case msg.on:
		m.status = "watching " + msg.key
	default:
		m.status = "stopped watching " + msg.key
	}
	return m, nil
}

// issueProject is the project part of an issue key.
func issueProject(key string) string {
	if i := strings.LastIndexByte(key, '-'); i > 0 {
		return key[:i]
	}
	return key
}
