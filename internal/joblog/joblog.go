// Package joblog reads a CI job's log the way GitLab's job view shows it:
// lines of styled text. Colours (SGR) become a span's style, a carriage
// return starts the line over (a progress bar's last state is what stays),
// GitLab's section markers and every other escape sequence are left out, and
// so are control characters: what is left is safe to draw. The web renders
// the spans as HTML, the TUI as its own SGR.
package joblog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cornedor/laneway/internal/safeterm"
)

// Style is a span's look. A colour is "0"–"15" (the terminal's named ones,
// 8–15 bright) or "#rrggbb", "" for the default.
type Style struct {
	FG, BG                       string
	Bold, Dim, Italic, Underline bool
}

// Span is a run of text in one style.
type Span struct {
	Text  string
	Style Style
}

// Line is one line of the log.
type Line []Span

// sectionRe is a GitLab section marker: section_start:<time>:<name>[opts]
// and section_end, each closed by a carriage return.
var sectionRe = regexp.MustCompile(`section_(?:start|end):\d+:[^\r\n]*?\r`)

// Parse is log as lines.
func Parse(log string) []Line {
	log = sectionRe.ReplaceAllString(strings.ReplaceAll(log, "\r\n", "\n"), "")
	var out []Line
	var st Style
	var line Line
	for i := 0; i < len(log); {
		switch c := log[i]; {
		case c == '\n':
			out, line = append(out, line), nil
			i++
		case c == '\r': // the line over: what follows replaces what came
			line = nil
			i++
		case c == 0x1b && i+1 < len(log) && log[i+1] == '[': // CSI: SGR applies, the rest goes
			j := i + 2
			for j < len(log) && (log[j] < 0x40 || log[j] > 0x7e) {
				j++
			}
			if j < len(log) && log[j] == 'm' {
				st.sgr(log[i+2 : j])
			}
			i = j + 1
		case c == 0x1b && i+1 < len(log) && log[i+1] == ']': // OSC, to BEL or ST
			j := i + 2
			for j < len(log) && log[j] != 0x07 && !(log[j] == 0x1b && j+1 < len(log) && log[j+1] == '\\') {
				j++
			}
			if j < len(log) && log[j] == 0x1b {
				j++
			}
			i = j + 1
		case c == 0x1b:
			i += 2
		default:
			j := i
			for j < len(log) && log[j] != '\n' && log[j] != '\r' && log[j] != 0x1b {
				j++
			}
			if t := safeterm.Line(strings.ReplaceAll(log[i:j], "\t", "    ")); t != "" {
				line = append(line, Span{Text: t, Style: st})
			}
			i = j
		}
	}
	if len(line) > 0 {
		out = append(out, line)
	}
	return out
}

// sgr applies one SGR sequence's parameters.
func (s *Style) sgr(params string) {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		n, _ := strconv.Atoi(ps[i]) // "" is 0: a reset
		ext := func() string {      // 38/48;5;n or 38/48;2;r;g;b
			if i+2 < len(ps) && ps[i+1] == "5" {
				v, _ := strconv.Atoi(ps[i+2])
				i += 2
				return xterm256(v)
			}
			if i+4 < len(ps) && ps[i+1] == "2" {
				r, _ := strconv.Atoi(ps[i+2])
				g, _ := strconv.Atoi(ps[i+3])
				b, _ := strconv.Atoi(ps[i+4])
				i += 4
				return fmt.Sprintf("#%02x%02x%02x", r&255, g&255, b&255)
			}
			return ""
		}
		switch {
		case n == 0:
			*s = Style{}
		case n == 1:
			s.Bold = true
		case n == 2:
			s.Dim = true
		case n == 3:
			s.Italic = true
		case n == 4:
			s.Underline = true
		case n == 22:
			s.Bold, s.Dim = false, false
		case n == 23:
			s.Italic = false
		case n == 24:
			s.Underline = false
		case n >= 30 && n <= 37:
			s.FG = strconv.Itoa(n - 30)
		case n >= 90 && n <= 97:
			s.FG = strconv.Itoa(n - 90 + 8)
		case n == 38:
			s.FG = ext()
		case n == 39:
			s.FG = ""
		case n >= 40 && n <= 47:
			s.BG = strconv.Itoa(n - 40)
		case n >= 100 && n <= 107:
			s.BG = strconv.Itoa(n - 100 + 8)
		case n == 48:
			s.BG = ext()
		case n == 49:
			s.BG = ""
		}
	}
}

// xterm256 is colour n of the 256: a named one below 16, else as #rrggbb.
func xterm256(n int) string {
	if n < 16 {
		return strconv.Itoa(max(n, 0))
	}
	if n >= 232 {
		g := 8 + (min(n, 255)-232)*10
		return fmt.Sprintf("#%02x%02x%02x", g, g, g)
	}
	n -= 16
	lv := func(v int) int {
		if v == 0 {
			return 0
		}
		return 55 + v*40
	}
	return fmt.Sprintf("#%02x%02x%02x", lv(n/36), lv(n/6%6), lv(n%6))
}

// SGR is the escape sequence that sets style on a terminal, "" for the
// default.
func (s Style) SGR() string {
	var ps []string
	col := func(c string, base, bright, ext int) {
		switch {
		case c == "":
		case strings.HasPrefix(c, "#") && len(c) == 7:
			r, _ := strconv.ParseUint(c[1:3], 16, 8)
			g, _ := strconv.ParseUint(c[3:5], 16, 8)
			b, _ := strconv.ParseUint(c[5:7], 16, 8)
			ps = append(ps, fmt.Sprintf("%d;2;%d;%d;%d", ext, r, g, b))
		default:
			n, _ := strconv.Atoi(c)
			if n < 8 {
				ps = append(ps, strconv.Itoa(base+n))
			} else {
				ps = append(ps, strconv.Itoa(bright+n-8))
			}
		}
	}
	for _, f := range []struct {
		on   bool
		code string
	}{{s.Bold, "1"}, {s.Dim, "2"}, {s.Italic, "3"}, {s.Underline, "4"}} {
		if f.on {
			ps = append(ps, f.code)
		}
	}
	col(s.FG, 30, 90, 38)
	col(s.BG, 40, 100, 48)
	if len(ps) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(ps, ";") + "m"
}

// ANSI is the line as terminal text: each styled span in its own SGR, reset
// after it.
func (l Line) ANSI() string {
	var b strings.Builder
	for _, sp := range l {
		if sgr := sp.Style.SGR(); sgr != "" {
			b.WriteString(sgr + sp.Text + "\x1b[0m")
			continue
		}
		b.WriteString(sp.Text)
	}
	return b.String()
}

// Plain is the line's text.
func (l Line) Plain() string {
	var b strings.Builder
	for _, sp := range l {
		b.WriteString(sp.Text)
	}
	return b.String()
}
