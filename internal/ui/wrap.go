package ui

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/textwidth"
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
//
// An OSC 8 link left open at a break is closed there and reopened on the next
// row, so the border and gutter between them are not part of it.
func carryStyle(parts []string) {
	var active, link string // SGR still open, and the link, at the previous row's end
	for i := range parts {
		parts[i] = link + active + parts[i]
		active, link = sgrState(parts[i]), openLink(parts[i])
		if i < len(parts)-1 {
			if active != "" {
				parts[i] += "\x1b[0m"
			}
			if link != "" {
				parts[i] += osc8Close
			}
		}
	}
}

const osc8Close = "\x1b]8;;\x1b\\"

// openLink is the OSC 8 opener still in effect at the end of s, "" when no
// link is open.
func openLink(s string) string {
	i := strings.LastIndex(s, "\x1b]8;")
	if i < 0 {
		return ""
	}
	end := strings.Index(s[i:], "\x1b\\")
	if end < 0 {
		return ""
	}
	seq := s[i : i+end+2]
	if strings.HasSuffix(seq, ";\x1b\\") { // "\x1b]8;;\x1b\\" closes: no URL
		return ""
	}
	return seq
}

// wrapPanel word-wraps every line of the issue panel's content wider than
// width. A wrapped line hangs its continuation rows under its text: after its
// gutter, quote and reply bars, and a list item's marker.
func wrapPanel(content string, width int) string {
	if width < 1 {
		return content
	}
	lines := strings.Split(content, "\n")
	out := lines[:0:0]
	for _, l := range lines {
		if textwidth.Width(l) <= width {
			out = append(out, l)
			continue
		}
		out = append(out, wrapHanging(l, width)...)
	}
	return strings.Join(out, "\n")
}

// listMarker is a list item's bullet or number and the space after it.
var listMarker = regexp.MustCompile(`^([-*+•]|\d{1,3}[.)]|\[[ xX]\]) +`)

// panelWrapMin is the fewest cells a continuation row's text gets; a hang
// deeper than that is dropped rather than wrap a word a row.
const panelWrapMin = 12

func wrapHanging(line string, width int) []string {
	// The prefix is the leading run of blanks and bars, with their styling.
	k, prefixW := 0, 0
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			n := escLen(line[i:])
			if n == 0 {
				break
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r != ' ' && r != '│' && r != '┃' {
			break
		}
		i += size
		k, prefixW = i, prefixW+1
	}
	if k == 0 {
		// Unindented rows (the hint row, bylines) are found by their text;
		// the viewport soft-wraps them.
		return []string{line}
	}
	prefix, body := line[:k], line[k:]
	marker := ""
	if loc := listMarker.FindStringIndex(body); loc != nil {
		marker, body = body[:loc[1]], body[loc[1]:]
	}
	hang := prefixW + textwidth.Width(marker)
	if width-hang < panelWrapMin {
		prefix, marker, body, prefixW, hang = "", "", line, 0, 0
	}
	parts := strings.Split(wrapWords(body, width-hang), "\n")
	carryStyle(parts)
	cont := sgrFree(prefix) + strings.Repeat(" ", hang-prefixW)
	for i := range parts {
		if i < len(parts)-1 {
			parts[i] += softMark
		}
		if i == 0 {
			parts[i] = prefix + marker + parts[i]
			continue
		}
		parts[i] = cont + parts[i]
	}
	return parts
}

// softMark ends a row a wrap continues on the next, as an OSC no terminal
// acts on (zero-width to the stages between); softRows takes it off.
const softMark = "\x1b]5379;soft\x1b\\"

// softRows is content without its soft marks, and which of its rows had one.
func softRows(content string) (string, []bool) {
	lines := strings.Split(content, "\n")
	soft := make([]bool, len(lines))
	if !strings.Contains(content, softMark) {
		return content, soft
	}
	for i, l := range lines {
		lines[i], soft[i] = strings.CutSuffix(l, softMark)
	}
	return strings.Join(lines, "\n"), soft
}

// nbHyphen stands in for "-" while wrapping: ansi.Wrap breaks after every
// hyphen, and an issue key (ABC-12) or a path must not split there.
const nbHyphen = "\u2011"

// wrapWords word-wraps s to width, breaking a word only when it is wider
// than a row.
func wrapWords(s string, width int) string {
	if strings.Contains(s, nbHyphen) {
		return ansi.Wrap(s, width, "")
	}
	s = ansi.Wrap(strings.ReplaceAll(s, "-", nbHyphen), width, "")
	return strings.ReplaceAll(s, nbHyphen, "-")
}

// sgrFree is a prefix fit to repeat on a continuation row: its bars keep
// their colour and nothing it opens stays open.
func sgrFree(prefix string) string {
	if sgrState(prefix) == "" {
		return prefix
	}
	return prefix + "\x1b[0m"
}

// escLen is the length of the CSI or OSC escape at the start of s, 0 for
// anything else.
func escLen(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch s[1] {
	case '[':
		for j := 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
	case ']':
		if j := strings.Index(s, "\x1b\\"); j > 0 {
			return j + 2
		}
		if j := strings.IndexByte(s, 0x07); j > 0 {
			return j + 1
		}
	}
	return 0
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
