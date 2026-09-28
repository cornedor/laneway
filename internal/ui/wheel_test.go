package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestWheelFilter: a burst's first notch goes through, the rest wait for
// the frame's end as one batch; a quiet frame lets the next one through.
func TestWheelFilter(t *testing.T) {
	c := &wheelCoalescer{send: func(tea.Msg) {}}
	down := tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelUp}
	if got := c.filter(down); got != tea.Msg(down) {
		t.Fatalf("first notch: %#v", got)
	}
	for range 4 {
		if got := c.filter(down); got != nil {
			t.Fatalf("held notch: %#v", got)
		}
	}
	c.filter(up)
	if got := c.filter(wheelFlushMsg{}); got != tea.Msg(wheelBatchMsg{wheel: down, n: 3}) {
		t.Fatalf("flush: %#v", got)
	}
	c.filter(up)
	c.filter(up)
	moved := tea.MouseWheelMsg{X: 9, Y: 3, Button: tea.MouseWheelDown}
	if got := c.filter(moved); got != tea.Msg(wheelBatchMsg{wheel: up, n: 2}) {
		t.Fatalf("moved: %#v", got)
	}
	if got := c.filter(wheelFlushMsg{}); got != tea.Msg(wheelBatchMsg{wheel: moved, n: 1}) {
		t.Fatalf("flush after move: %#v", got)
	}
	if got := c.filter(wheelFlushMsg{}); got != nil || c.open {
		t.Fatalf("quiet frame: %#v open=%v", got, c.open)
	}
	if got := c.filter(up); got != tea.Msg(up) {
		t.Fatalf("after quiet: %#v", got)
	}
	left := tea.MouseWheelMsg{Button: tea.MouseWheelLeft}
	if got := c.filter(left); got != tea.Msg(left) {
		t.Fatalf("sideways: %#v", got)
	}
}

// TestWheelBatchScrollsPanel: a batch scrolls the panel as far as its
// notches one by one.
func TestWheelBatchScrollsPanel(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	out, _ = openRefFor(out.(Model), "ABC-1")
	m = out.(Model)
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	m.refView.SetContentLines(lines)
	listW, _ := m.jiraListWidth(m.width)
	out, _ = m.Update(wheelBatchMsg{wheel: tea.MouseWheelMsg{X: listW + 2, Y: 5, Button: tea.MouseWheelDown}, n: 4})
	m = out.(Model)
	if got := m.refView.YOffset(); got != 12 {
		t.Fatalf("offset %d, want 12", got)
	}
}
