package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestMarkdownLinkText: a link's text gets inline styling too, and a code
// span in it shows as code, not a stash sentinel.
func TestMarkdownLinkText(t *testing.T) {
	for in, want := range map[string]string{
		"see [`code`](https://x.test/a)":   "see code",
		"see [**bold**](https://x.test/a)": "see bold",
		"https://x.test/a_b_c":             "https://x.test/a_b_c",
	} {
		got := renderInline(in, nil, nil, "")
		if strings.Contains(got, "\x00") || strings.Contains(got, "MDCODE") {
			t.Errorf("%q leaks a sentinel: %q", in, got)
		}
		if plain := ansi.Strip(got); plain != want {
			t.Errorf("%q = %q, want %q", in, plain, want)
		}
	}
}
