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
			row(k.Roadmap, "roadmap: epics on a timeline (space children, H L < > dates)"),
			row(k.Sort, "sort the list"),
			{join(k.MoveCardLeft, k.MoveCardRight), "move card a lane"},
			row(k.MoveSprint, "move to sprint / backlog"),
			row(k.Search, "search (esc clears)"),
			{join(k.Assignee, k.Mine), "assignee filter / mine"},
			{"1-9 / " + helpKey(k.ClearFilters), "quick filter / clear"},
			row(k.Refresh, "refresh"),
			row(k.Tab, "to panel"),
			row(k.Quit, "quit"),
		}},
		{"Panel", []helpRow{
			row(k.JiraStatus, "status"),
			row(k.JiraPriority, "priority"),
			row(k.JiraPoints, "story points"),
			row(k.JiraSummary, "edit summary"),
			row(k.JiraLabels, "edit labels"),
			row(k.JiraAssignee, "assignee"),
			{join(k.JiraComment, k.JiraReply), "comment / reply"},
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

func (m *Model) renderHelp() string {
	keyStyle := lipgloss.NewStyle().Foreground(focusedColor).Bold(true)
	var cols []string
	for _, s := range m.helpSections() {
		keyW := 0
		for _, r := range s.rows {
			keyW = max(keyW, lipgloss.Width(r.keys))
		}
		lines := []string{titleStyle.Render(s.title), ""}
		for _, r := range s.rows {
			pad := strings.Repeat(" ", keyW-lipgloss.Width(r.keys))
			lines = append(lines, keyStyle.Render(r.keys)+pad+"  "+r.desc)
		}
		cols = append(cols, strings.Join(lines, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols[0], "     ", cols[1])
	hint := lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("any key closes · rebind in ui.keys")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).
		Padding(1, 3).Render(lipgloss.JoinVertical(lipgloss.Left, body, "", hint))
}
