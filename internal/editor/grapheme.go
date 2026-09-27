package editor

import "github.com/rivo/uniseg"

// clusterBefore is the start of the grapheme cluster ending at (or holding)
// col in line: where one step left lands, so a flag or ZWJ family moves and
// deletes as the one character it draws as.
func clusterBefore(line []rune, col int) int {
	pos := 0
	g := uniseg.NewGraphemes(string(line))
	for g.Next() {
		n := len(g.Runes())
		if pos+n >= col {
			return pos
		}
		pos += n
	}
	return pos
}

// clusterAfter is the end of the grapheme cluster starting at (or holding)
// col in line: where one step right lands.
func clusterAfter(line []rune, col int) int {
	pos := 0
	g := uniseg.NewGraphemes(string(line))
	for g.Next() {
		n := len(g.Runes())
		if pos+n > col {
			return pos + n
		}
		pos += n
	}
	return pos
}
