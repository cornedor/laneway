// Package safeterm strips terminal control characters out of untrusted text.
//
// Anything a remote peer authored — message bodies, usernames, channel and
// team names, emoji names, filenames, custom-status text — ends up on a
// terminal that interprets escape sequences. A raw ESC in that text lets the
// sender clear or repaint the screen, spoof a shell prompt, or write the
// user's clipboard with OSC 52. Run every such string through Text or Line
// before it reaches a render path.
package safeterm

import "strings"

// Text returns s with terminal control characters removed. Newline and tab
// survive (they are ordinary layout in a multi-line message body);
// everything else below U+0020, DEL, and the C1 block U+0080–U+009F — which
// carries single-byte CSI (U+009B) and OSC (U+009D) — is dropped.
func Text(s string) string { return strip(s, true) }

// Line is Text for single-line fields: newline and tab go too, so a value
// can never break out of the line it is rendered on.
func Line(s string) string { return strip(s, false) }

func strip(s string, keepLayout bool) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return unsafeRune(r, keepLayout) }) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unsafeRune(r, keepLayout) {
			return -1
		}
		return r
	}, s)
}

func unsafeRune(r rune, keepLayout bool) bool {
	if keepLayout && (r == '\n' || r == '\t') {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// JSON drops terminal control characters from the strings in a JSON
// document, before it is decoded: raw C1 runes, and \u escapes that decode
// to one. \n and \t escapes survive, as in Text. It is a catch-all for a
// peer's whole API response, so a field no render path thought to clean
// still can't carry an escape.
func JSON(b []byte) []byte {
	var out []byte // nil until something is dropped
	for i := 0; i < len(b); i++ {
		n := 0 // bytes to drop at i
		switch {
		case b[i] == '\\' && i+1 < len(b) && b[i+1] == 'u' && i+6 <= len(b):
			if r, ok := hex4(b[i+2 : i+6]); ok && unsafeRune(r, true) {
				n = 6
			}
		case b[i] == '\\' && i+1 < len(b):
			// Any other escape, \\ included, is kept whole so its second
			// byte is not read as the start of one.
			if out != nil {
				out = append(out, b[i], b[i+1])
			}
			i++
			continue
		case b[i] == 0xc2 && i+1 < len(b) && b[i+1] >= 0x80 && b[i+1] <= 0x9f:
			n = 2
		}
		if n == 0 {
			if out != nil {
				out = append(out, b[i])
			}
			continue
		}
		if out == nil {
			out = append(make([]byte, 0, len(b)), b[:i]...)
		}
		i += n - 1
	}
	if out == nil {
		return b
	}
	return out
}

func hex4(h []byte) (rune, bool) {
	var r rune
	for _, c := range h {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}
