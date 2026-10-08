package ui

import "testing"

func TestHighlightSpans(t *testing.T) {
	sp := HighlightSpans("TypeScript", "function xyz() {\n  return \"é\";\n}")
	got := map[string]bool{}
	for _, s := range sp {
		got[s.Class] = true
	}
	if !got["hl-k"] || !got["hl-s"] {
		t.Errorf("spans = %+v, want a keyword and a string", sp)
	}
	// Offsets count UTF-16 units: the string "é" is 3 long.
	for _, s := range sp {
		if s.Class == "hl-s" && s.To-s.From != 3 {
			t.Errorf("string span %+v, want 3 units", s)
		}
	}
	if HighlightSpans("no-such-language", "x") != nil {
		t.Error("an unknown language highlights")
	}
}
