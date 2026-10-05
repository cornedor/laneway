package tui

import (
	"testing"
	"time"
)

// TestBoardAndQuit: the active sprint in lanes, and q ends laneway.
func TestBoardAndQuit(t *testing.T) {
	s := start(t)
	for _, lane := range []string{"To Do 3 · 6p", "In Progress 2/4 · 8p", "In Review 3 · 6p", "Done 2 · 5p"} {
		s.wait(lane)
	}
	s.keys("q")
	for end := time.Now().Add(5 * time.Second); s.running(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("q left laneway running:\n%s", s.screen())
		}
	}
}

// TestMoveCard: L moves the selected card a lane on.
func TestMoveCard(t *testing.T) {
	s := start(t)
	s.keys("L")
	s.wait("To Do 2 · 3p")
	s.wait("In Progress 3/4 · 11p")
}

// TestStatus: s in the panel moves the issue to Done.
func TestStatus(t *testing.T) {
	s := start(t)
	s.keys("Enter")
	s.wait("DEMO-5  Story")
	s.keys("s")
	s.wait("↵ apply · esc cancel")
	s.keys("Down", "Down", "Down", "Down", "Enter")
	s.wait("Status:    DONE")
	s.wait("Done 3 · 8p")
}

// TestComment: c writes a comment, ctrl+s posts it.
func TestComment(t *testing.T) {
	s := start(t)
	s.keys("Enter")
	s.wait("no comments yet")
	s.keys("c")
	s.wait("ctrl+s post")
	s.typ("Ask Tomás about the address book")
	s.keys("C-s")
	s.wait("Comments (1)")
	s.wait("Ask Tomás about the address book")
}

// TestCreate: n opens the form, enter creates into the sprint shown.
func TestCreate(t *testing.T) {
	s := start(t)
	s.keys("n")
	s.wait("New Task in DEMO")
	s.typ("Gift cards at checkout")
	s.keys("Enter")
	s.gone("New Task in DEMO")
	s.wait("To Do 4")
	s.wait("Gift cards at checkout")
}

// TestViews: each view opens from the board, draws from the demo and esc
// goes back.
func TestViews(t *testing.T) {
	s := start(t)
	for _, v := range []struct{ key, shows string }{
		{"P", "Planning  backlog → Sprint 13"},
		{"C", "Burndown"},
		{"R", "Roadmap  3 epics"},
		{"I", "Mentions"},
		{"U", "Standup · since"},
		{"?", "move card a lane"},
	} {
		s.keys(v.key)
		s.wait(v.shows)
		s.keys("Escape")
		s.wait("To Do 3 · 6p")
	}
}
