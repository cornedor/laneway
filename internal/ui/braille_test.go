package ui

import (
	"slices"
	"testing"
)

func TestBraille(t *testing.T) {
	c := newBraille(2, 1)
	c.set(0, 0)
	c.set(3, 3)
	c.set(9, 9) // off canvas
	if got := c.rows(); !slices.Equal(got, []string{"⠁⢀"}) {
		t.Errorf("rows = %q", got)
	}
	c = newBraille(2, 1)
	c.line(0, 3, 3, 0, 1)
	if got := c.rows(); !slices.Equal(got, []string{"⡠⠊"}) {
		t.Errorf("line = %q", got)
	}
}
