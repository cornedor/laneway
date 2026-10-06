package tui

import (
	"strings"
	"testing"
)

// TestPlanMove: M moves a backlog card into the sprint, and back.
func TestPlanMove(t *testing.T) {
	s := start(t)
	s.keys("P")
	s.wait("Backlog  4 cards")
	s.keys("M")
	s.wait("Backlog  3 cards")
	s.wait("Sprint 13  4 cards")
	s.keys("Right", "Down", "Down", "Down") // DEMO-17, last in the sprint
	s.keys("M")
	s.wait("Backlog  4 cards")
	s.wait("Sprint 13  3 cards")
}

// TestPlanRank: J ranks the first backlog card down, and Jira keeps it.
func TestPlanRank(t *testing.T) {
	s := start(t)
	s.keys("P")
	s.wait("Backlog  4 cards")
	s.keys("J")
	below := func(scr string) bool { return strings.Index(scr, "DEMO-18") < strings.Index(scr, "DEMO-17") }
	s.until(below, "DEMO-17 not under DEMO-18")
	s.keys("Escape")
	s.wait("To Do 3 · 6p")
	s.keys("P")
	s.wait("Backlog  4 cards")
	if scr := s.screen(); !below(scr) {
		t.Fatalf("rank gone after reopening:\n%s", scr)
	}
}

// TestPlanGoal: E sets the sprint's goal; its board shows it.
func TestPlanGoal(t *testing.T) {
	s := start(t)
	s.keys("P")
	s.wait("backlog → Sprint 13")
	s.keys("Right")
	s.keys("E")
	s.wait("Goal of Sprint 13")
	s.typ("Ship invoices")
	s.keys("Enter")
	s.wait("Sprint 13 goal set")
	s.keys("]")
	s.wait("Ship invoices")
}

// TestPlanCompleteStart: C twice completes the active sprint, its open
// issues going to the next; S starts that one.
func TestPlanCompleteStart(t *testing.T) {
	s := start(t)
	s.keys("P")
	s.wait("backlog → Sprint 13")
	s.keys("[")
	s.wait("backlog → Sprint 12 - Checkout")
	s.keys("C")
	s.wait("C again completes Sprint 12 - Checkout")
	s.keys("C")
	s.wait("Sprint 12 - Checkout completed, 8 unfinished to Sprint 13")
	s.keys("P")
	s.wait("Sprint 13  11 cards")
	s.keys("S")
	s.wait("Start Sprint 13 today")
	s.keys("Enter")
	s.wait("d left") // 14 or 15: the end is a date, the start now
	s.wait("To Do 6 · 16p")
	s.wait("Done 0")
}
