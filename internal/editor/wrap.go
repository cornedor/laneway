package editor

import (
	"strings"
	"unicode"

	"github.com/cornedor/laneway/internal/textwidth"
	"github.com/rivo/uniseg"
)

// wrapLine soft-wraps one logical line into visual sub-lines at the given cell
// width, using a greedy word wrap that breaks words longer than the width. The
// concatenation of the returned sub-lines equals the input exactly (every rune,
// including spaces, is preserved and in order) — this is what lets the layout
// map a rune offset to a visual (sub-line, column) position unambiguously.
//
// reserve keeps that many cells free at the very end of the last sub-line, so a
// caret parked at end-of-line has somewhere to sit. This mirrors the synthetic
// trailing space charm.land/bubbles/v2 textarea's wrap() appended: with
// reserve=1, a trailing word that exactly fills the row wraps down as a unit
// (carrying the caret with it) instead of leaving a bare caret stranded on the
// next row. The reserved cell is virtual — it never enters the returned content,
// so the sub-lines stay a faithful slice of the source.
func wrapLine(runes []rune, width, reserve int) [][]rune {
	if width <= 0 {
		return [][]rune{append([]rune(nil), runes...)}
	}
	w := func(rs []rune) int { return textwidth.Width(string(rs)) }

	var (
		lines  = [][]rune{{}}
		word   []rune
		lastW  int // width of word's last cluster
		row    int
		spaces int
	)
	// Step by grapheme cluster, so a flag or ZWJ family is never split across
	// rows.
	g := uniseg.NewGraphemes(string(runes))
	for g.Next() {
		c := g.Runes()
		if len(c) == 1 && unicode.IsSpace(c[0]) {
			spaces++
		} else {
			cw := w(c)
			if len(word) > 0 && w(word)+cw > width {
				// A wide cluster that no longer fits the row breaks the word
				// before it.
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
			word = append(word, c...)
			lastW = cw
		}
		if spaces > 0 {
			// A run of spaces closes the pending word; flush word+spaces,
			// wrapping to a new row first if they no longer fit.
			if w(lines[row])+w(word)+spaces > width {
				row++
				lines = append(lines, []rune{})
			}
			lines[row] = append(lines[row], word...)
			lines[row] = append(lines[row], spacesRunes(spaces)...)
			spaces = 0
			word = nil
		} else if len(word) > 0 {
			// No spaces yet: a word that on its own would overflow the row
			// (accounting for its last cluster's width) starts a fresh row.
			if w(word)+lastW > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}
	// Flush the trailing word / spaces, reserving cells for the end-of-line caret.
	if w(lines[row])+w(word)+spaces+reserve > width {
		lines = append(lines, []rune{})
		row++
	}
	lines[row] = append(lines[row], word...)
	lines[row] = append(lines[row], spacesRunes(spaces)...)
	return lines
}

func spacesRunes(n int) []rune {
	if n <= 0 {
		return nil
	}
	return []rune(strings.Repeat(" ", n))
}
