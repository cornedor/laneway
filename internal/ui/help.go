package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The ? overlay: every key of the board and the panel. Any key closes it.

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Board", [][2]string{
		{"↑↓ ←→ / hjkl", "move"},
		{"enter", "open issue"},
		{"#", "go to issue by key"},
		{"o", "open in browser"},
		{"y / Y", "copy key / URL"},
		{"p / b", "project / board"},
		{"[ ]", "previous / next view"},
		{"t", "lanes / list"},
		{"H L", "move card a lane"},
		{"/", "search (esc clears)"},
		{"a", "assignee filter"},
		{"1-9 / 0", "quick filter / clear"},
		{"r", "refresh"},
		{"tab", "to panel"},
		{"q", "quit"},
	}},
	{"Panel", [][2]string{
		{"s", "status"},
		{"p", "priority"},
		{"P", "story points"},
		{"a", "assignee"},
		{"c / R", "comment / reply"},
		{"S", "start work"},
		{"o", "open in browser"},
		{"y / Y", "copy key / URL"},
		{"r", "refresh"},
		{"tab", "to board"},
		{"esc", "close"},
	}},
}

func (m *Model) renderHelp() string {
	keyStyle := lipgloss.NewStyle().Foreground(focusedColor).Bold(true)
	var cols []string
	for _, s := range helpSections {
		keyW := 0
		for _, k := range s.keys {
			keyW = max(keyW, lipgloss.Width(k[0]))
		}
		lines := []string{titleStyle.Render(s.title), ""}
		for _, k := range s.keys {
			pad := strings.Repeat(" ", keyW-lipgloss.Width(k[0]))
			lines = append(lines, keyStyle.Render(k[0])+pad+"  "+k[1])
		}
		cols = append(cols, strings.Join(lines, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols[0], "     ", cols[1])
	hint := lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("any key closes")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).
		Padding(1, 3).Render(lipgloss.JoinVertical(lipgloss.Left, body, "", hint))
}
