package ui

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// TestPanelBoxMatchesLipgloss: the panel's box and the joins are what
// lipgloss draws, byte for byte.
func TestPanelBoxMatchesLipgloss(t *testing.T) {
	content := "title\n" + refDimStyle.Render("dim ─ line") + "\n\nwide ✓ 日本"
	got, ok := renderPanelBox(content, 20, 8, focusedColor)
	want := lipgloss.NewStyle().Border(border).UnsetBorderTop().UnsetBorderRight().
		Width(20).Height(8).BorderForeground(focusedColor).Render(content)
	if !ok || got != want {
		t.Fatalf("box:\n%q\nwant\n%q", got, want)
	}
	got, ok = renderPaneBox(content, 20, 8, dimColor)
	want = lipgloss.NewStyle().Border(border).UnsetBorderTop().
		Width(20).Height(8).BorderForeground(dimColor).Render(content)
	if !ok || got != want {
		t.Fatalf("pane box:\n%q\nwant\n%q", got, want)
	}

	right := renderRightBorder(8, 1, 6, 20, 0.5, dimColor, true, -1)
	for _, left := range []string{want} {
		got, ok := joinBeside(left, right)
		if w := lipgloss.JoinHorizontal(lipgloss.Top, left, right); !ok || got != w {
			t.Fatalf("beside:\n%q\nwant\n%q", got, w)
		}
	}
	short := "ab\ncd"
	if got, ok := joinBeside(short, right); !ok || got != lipgloss.JoinHorizontal(lipgloss.Top, short, right) {
		t.Fatalf("short beside:\n%q", got)
	}
	if _, ok := joinBeside("a\nbb", right); ok {
		t.Fatal("ragged block joined")
	}

	status := statusStyle.Render(" status")
	if got, ok := joinVerticalLeft(want, status); !ok || got != lipgloss.JoinVertical(lipgloss.Left, want, status) {
		t.Fatalf("vertical:\n%q", got)
	}
}
