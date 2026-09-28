package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/textwidth"
)

func TestWrapPanel(t *testing.T) {
	bar := refDimStyle.Render("│ ")
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{"fits", "  short line", []string{"  short line"}},
		{"words", "  the order total is off by a cent here",
			[]string{"  the order total is", "  off by a cent here"}},
		{"list", "  1. Add two items totalling EUR to it",
			[]string{"  1. Add two items", "     totalling EUR to", "     it"}},
		{"bullet", "  - pay half on a card and half on credit",
			[]string{"  - pay half on a card", "    and half on credit"}},
		{"quote", "  ┃ quoted words that run past the edge",
			[]string{"  ┃ quoted words that", "  ┃ run past the edge"}},
		{"reply", bar + "  a reply that runs past the edge",
			[]string{"│   a reply that runs", "│   past the edge"}},
		{"long word", "  " + strings.Repeat("x", 30),
			[]string{"  " + strings.Repeat("x", 20), "  " + strings.Repeat("x", 10)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Split(wrapPanel(tc.in, 22), "\n")
			for i := range got {
				if w := textwidth.Width(got[i]); w > 22 {
					t.Errorf("row %d is %d wide: %q", i, w, got[i])
				}
				got[i] = ansi.Strip(got[i])
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// A style or link spanning a break closes before it and reopens after, so
// the panel's border between the rows is neither coloured nor a link.
func TestWrapPanelCarries(t *testing.T) {
	link := osc8Link("https://example.com", "a link that spans the break")
	rows := strings.Split(wrapPanel("  see \x1b[1m"+link+"\x1b[m now", 22), "\n")
	if len(rows) < 2 {
		t.Fatalf("rows = %q, want a wrap", rows)
	}
	for i, r := range rows {
		if i < len(rows)-1 && (openLink(r) != "" || sgrState(r) != "") {
			t.Errorf("row %d leaves a link or style open: %q", i, r)
		}
		if i > 0 && !strings.Contains(r, "\x1b]8;;https://example.com") {
			t.Errorf("row %d lost the link: %q", i, r)
		}
	}
}

func TestWrapPanelKeepsHyphens(t *testing.T) {
	got := ansi.Strip(wrapPanel("  seen after the change in NOVA-96 landed", 30))
	if want := "  seen after the change in\n  NOVA-96 landed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := wrapPanel("tab fields · ↵ edit · c comment · R reply", 22); strings.Contains(got, "\n") {
		t.Errorf("an unindented row wrapped: %q", got)
	}
}
