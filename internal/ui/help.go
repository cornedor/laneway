package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// The ? overlay: every key of the board and the panel, as bound (ui.keys
// rebinds them). Any key closes it.

type helpRow struct {
	keys string
	desc string
}

func row(b key.Binding, desc string) helpRow { return helpRow{keysLabel(b), desc} }

func (m *Model) helpSections() []struct {
	title string
	rows  []helpRow
} {
	k := m.keys
	join := func(a, b key.Binding) string { return helpKey(a) + " / " + helpKey(b) }
	return []struct {
		title string
		rows  []helpRow
	}{
		{"Board", []helpRow{
			{join(k.Up, k.Down) + "  " + join(k.Left, k.Right), "move"},
			row(k.OpenChannel, "open issue"),
			row(k.Goto, "go to issue by key"),
			row(k.Create, "new issue"),
			row(k.OpenAttach, "open in browser"),
			{join(k.CopyKey, k.CopyURL), "copy key / URL"},
			{join(k.Project, k.Board), "project / board"},
			{join(k.PrevView, k.NextView), "previous / next view"},
			row(k.ToggleMode, "lanes / list"),
			row(k.Palette, "command palette"),
			row(k.JQL, "JQL search with completion, as a view"),
			{join(k.Timer, k.Timesheet), "timer / today's worklogs"},
			row(k.Inbox, "inbox: others' changes, comments, mentions"),
			row(k.Standup, "standup: what you did since the last workday"),
			row(k.Charts, "sprint charts: burndown, velocity"),
			row(k.Plan, "sprint planning: backlog beside a sprint (K J rank)"),
			row(k.Roadmap, "roadmap: epics on a timeline (space children, H L < > dates)"),
			row(k.Sort, "sort the list"),
			{join(k.MoveCardLeft, k.MoveCardRight), "move card a lane"},
			row(k.MoveSprint, "move to sprint / backlog"),
			{join(k.Mark, k.Bulk), "mark card / edit marked (esc clears)"},
			row(k.MarkAll, "mark the lane / every row"),
			row(k.Search, "search (esc clears)"),
			{join(k.Assignee, k.Mine), "assignee filter / mine"},
			{"1-9 / " + helpKey(k.ClearFilters), "quick filter / clear"},
			row(k.Refresh, "refresh"),
			row(k.Tab, "to panel"),
			row(k.Site, "switch Jira site"),
			row(k.Quit, "quit"),
		}},
		{"Panel", []helpRow{
			row(k.JiraStatus, "status"),
			row(k.JiraPriority, "priority"),
			row(k.JiraPoints, "story points"),
			row(k.JiraSummary, "edit summary"),
			row(k.JiraLabels, "edit labels"),
			row(k.JiraDescription, "edit description in $EDITOR"),
			row(k.JiraAssignee, "assignee"),
			{join(k.JiraComment, k.JiraReply), "comment / reply"},
			{join(k.LogWork, k.Timer), "log work / timer"},
			row(k.Timesheet, "today's worklogs"),
			row(k.Inbox, "inbox"),
			row(k.IssueActions, "subtask / child, link, clone, watch"),
			row(k.History, "history: changes and comments"),
			row(k.DevInfo, "pull requests and branches"),
			row(k.JiraStart, "start work"),
			row(k.OpenAttach, "open in browser"),
			{join(k.CopyKey, k.CopyURL), "copy key / URL"},
			row(k.JiraLinks, "go to linked issue"),
			row(k.Image, "view images full size"),
			row(k.Back, "previous issue"),
			row(k.Refresh, "refresh"),
			row(k.Palette, "command palette"),
			{join(k.Tab, k.ShiftTab), "walk fields, then to board"},
			{"enter", "edit selected field"},
			{"esc", "drop field, close"},
		}},
	}
}

// renderHelp lays the sections out side by side, a section running on into
// another column when it is taller than height allows.
func (m *Model) renderHelp(height int) string {
	keyStyle := lipgloss.NewStyle().Foreground(focusedColor).Bold(true)
	perCol := max(height-10, 6) // border, padding, title and hint
	var cols []string
	for _, s := range m.helpSections() {
		keyW := 0
		for _, r := range s.rows {
			keyW = max(keyW, lipgloss.Width(r.keys))
		}
		for start := 0; start < len(s.rows); start += perCol {
			title := s.title
			if start > 0 {
				title += " (more)"
			}
			lines := []string{titleStyle.Render(title), ""}
			for _, r := range s.rows[start:min(start+perCol, len(s.rows))] {
				pad := strings.Repeat(" ", keyW-lipgloss.Width(r.keys))
				lines = append(lines, keyStyle.Render(r.keys)+pad+"  "+r.desc)
			}
			if len(cols) > 0 {
				cols = append(cols, "   ")
			}
			cols = append(cols, strings.Join(lines, "\n"))
		}
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	hint := lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("any key closes · rebind in ui.keys")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).
		Padding(1, 3).Render(lipgloss.JoinVertical(lipgloss.Left, body, "", hint))
}
