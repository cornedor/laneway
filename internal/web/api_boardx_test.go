package web

import "testing"

func TestAndOrderedJQL(t *testing.T) {
	for _, c := range []struct{ q, b, want string }{
		{"a = 1", "", "a = 1"},
		{"", "b = 2", "b = 2"},
		{"a = 1", "b = 2", "(a = 1) AND (b = 2)"},
		{"a = 1 ORDER BY rank", "b = 2", "(a = 1) AND (b = 2) ORDER BY rank"},
		{"ORDER BY rank", "b = 2", "b = 2 ORDER BY rank"},
	} {
		if got := andOrderedJQL(c.q, c.b); got != c.want {
			t.Errorf("andOrderedJQL(%q, %q) = %q, want %q", c.q, c.b, got, c.want)
		}
	}
}
