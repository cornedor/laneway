package tui

import (
	"strings"
	"testing"
)

// TestBulkUndo: x marks two cards, B assigns both, u takes it back.
func TestBulkUndo(t *testing.T) {
	s := start(t)
	s.keys("x", "x") // x steps on: DEMO-5, then DEMO-9
	s.keys("B")
	s.wait("Edit 2 marked")
	s.keys("3")
	s.wait("Assign to me")
	s.keys("Down", "Down", "Down", "Down", "Enter") // Sam Okafor
	sams := func(n int) func(string) bool {
		return func(scr string) bool { return strings.Count(lane(scr, 0), "SO Sam Okafor") == n }
	}
	s.until(sams(2), "not two cards for Sam in To Do")
	s.keys("u")
	s.until(sams(0), "undo left Sam on a card")
	s.waitLane(0, "TR Tomás Ruiz")
}
