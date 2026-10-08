package jira

import "testing"

// TestFieldPins: the stars kept before carry over as pins; a pin, on or
// off, is kept; a field without one keeps its default.
func TestFieldPins(t *testing.T) {
	s := memMeta{starredMeta: `["c1"]`}
	if pins := FieldPins(s); len(pins) != 1 || !pins["c1"] {
		t.Fatalf("from stars: %v", pins)
	}
	pins, err := SetFieldPin(s, "priority", false)
	if err != nil || len(pins) != 2 || !pins["c1"] {
		t.Fatalf("set: %v %v", pins, err)
	}
	pins = FieldPins(s)
	if Pinned(pins, "priority", true) || !Pinned(pins, "c1", false) || !Pinned(pins, "status", true) || Pinned(pins, "c2", false) {
		t.Errorf("pins = %v", pins)
	}
	if pins := FieldPins(nil); pins == nil || len(pins) != 0 {
		t.Errorf("no store: %v", pins)
	}
}
