package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSubtask: A's first action makes a subtask; the panel opens it under
// its parent.
func TestSubtask(t *testing.T) {
	s := panelOn(t)
	s.keys("A")
	s.wait("New subtask")
	s.keys("Enter")
	s.wait("New Sub-task of DEMO-5")
	s.typ("Store the address")
	s.keys("Enter")
	s.wait("↰ DEMO-5")
	s.wait("Store the address")
	s.wait("parent DEMO-5")
}

// TestLink: A links the issue to another; the panel lists the link.
func TestLink(t *testing.T) {
	s := panelOn(t)
	s.keys("A")
	s.wait("New subtask")
	s.keys("Down", "Enter")
	s.wait("Link DEMO-5")
	s.keys("Enter") // blocks …
	s.wait("a key, a number in DEMO")
	s.typ("DEMO-13")
	s.wait("Address form loses the house number")
	s.keys("Enter")
	s.wait("Links (2)")
	s.wait("blocks DEMO-13 Address form loses the house number")
}

// TestLogWork: w logs time; the Work log tab shows it.
func TestLogWork(t *testing.T) {
	s := panelOn(t)
	s.keys("w")
	s.wait("Log work — DEMO-5")
	s.typ("1h 30m Wireframes")
	s.keys("Enter")
	s.gone("Log work — DEMO-5")
	s.keys("]", "]") // Comments → History → Work log
	s.wait("logged 1h 30m")
	s.wait("Wireframes")
}

// TestTimer: T starts timing; T again offers to log it, ctrl+d drops it.
func TestTimer(t *testing.T) {
	s := panelOn(t)
	s.keys("T")
	s.wait("⏱ DEMO-5")
	s.keys("T")
	s.wait("ctrl+d drop the timer")
	s.keys("C-d")
	s.gone("⏱")
}

// TestHistory: an edit lands in the History tab and in H's list.
func TestHistory(t *testing.T) {
	s := panelOn(t)
	s.keys("p")
	s.wait("Lowest")
	s.keys("Up", "Enter")
	s.wait("Priority: High")
	s.keys("]")
	s.wait("priority Medium → High")
	s.keys("H")
	s.wait("History — DEMO-5")
	s.wait("priority: Medium → High")
}

// TestAttach: A uploads a file by path; the panel lists it.
func TestAttach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := panelOn(t)
	s.keys("A")
	s.wait("New subtask")
	s.keys("End", "Up", "Up", "Enter") // Upload a file
	s.wait("Upload a file — DEMO-5")
	s.typ(path)
	s.keys("Enter")
	s.wait("Attachments (1)")
	s.wait("notes.txt  6 B")
}
