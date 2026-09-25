package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Soft-wrapping of rendered body lines, from matterbox's view.go.

func wrapBodyLine(line string, width int) []string {
	if width < 4 || lipgloss.Width(line) <= width {
		return []string{line}
	}
	const indent = "  "
	if !strings.HasPrefix(line, indent) {
		return []string{line}
	}
	wrapped := ansi.Wrap(line[len(indent):], width-len(indent), "")
	parts := strings.Split(wrapped, "\n")
	carryStyle(parts)
	for i, p := range parts {
		parts[i] = indent + p
	}
	return parts
}

// carryStyle fixes up the rows produced by an ANSI-aware soft-wrap so colour
// survives the break. ansi.Wrap keeps escape codes where they sit but never
// re-opens a style on the continuation row, so a single coloured span (e.g. a
// long code comment) loses its colour the moment it wraps — only the visual row
// that physically contains the opening SGR is painted. carryStyle re-emits the
// style left open at each break at the start of the next row, and reset-
// terminates every row but the last so the colour can't bleed into the gutter
// below. Rows whose span already started on a fresh line (a string literal, an
// identifier) are unaffected — they carry their own opening SGR.
func carryStyle(parts []string) {
	var active string // SGR sequence(s) still open at the end of the previous row
	for i := range parts {
		if active != "" {
			parts[i] = active + parts[i]
		}
		active = sgrState(parts[i])
		if active != "" && i < len(parts)-1 {
			parts[i] += "\x1b[0m"
		}
	}
}

// sgrState returns the SGR escape sequence(s) left open at the end of s — the
// styling a continuation row must re-emit to keep painting. It tracks only SGR
// (ESC[…m) sequences: a reset (ESC[0m, ESC[m, or any params beginning "0;")
// clears the accumulated state; every other SGR is appended, so re-emitting the
// result reproduces the live style. chroma and lipgloss reset between tokens, so
// the accumulator stays short. Non-SGR escapes (cursor moves, links) are ignored.
func sgrState(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			i++
			continue
		}
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) { // CSI final byte is @–~
			j++
		}
		if j >= len(s) {
			break // truncated sequence; nothing more to track
		}
		if s[j] == 'm' { // an SGR sequence
			params := s[i+2 : j]
			if params == "" || params == "0" || strings.HasPrefix(params, "0;") {
				b.Reset()
			}
			if params != "" && params != "0" {
				b.WriteString(s[i : j+1])
			}
		}
		i = j + 1
	}
	return b.String()
}
