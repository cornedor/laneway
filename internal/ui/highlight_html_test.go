package ui

import (
	"strings"
	"testing"
)

// TestHighlightHTML: a line each, tokens classed and escaped; an unknown
// file type is escaped plain text.
func TestHighlightHTML(t *testing.T) {
	got := HighlightHTML("main.go", []string{"func f() int {", `	return "<a>" // x`, "}"})
	if len(got) != 3 || !strings.Contains(got[0], `<span class="hl-k">func</span>`) || !strings.Contains(got[0], `<span class="hl-t">int</span>`) ||
		!strings.Contains(got[1], `<span class="hl-s">&#34;&lt;a&gt;&#34;</span>`) || !strings.Contains(got[1], `<span class="hl-c">// x</span>`) {
		t.Errorf("go: %q", got)
	}
	if got := HighlightHTML("notes.unknownext", []string{"a <b>", ""}); got[0] != "a &lt;b&gt;" || got[1] != "" {
		t.Errorf("plain: %q", got)
	}
}
