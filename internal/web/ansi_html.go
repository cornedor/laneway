package web

import (
	"html"
	"strings"

	"github.com/cornedor/laneway/internal/joblog"
)

// ansiHTML is a CI job's log as HTML, the way GitLab's job view draws it
// (joblog reads it): one <div> a line, a styled span its classes (a-c1:
// colour 1, a-b1: background 1) or, for a 256 or RGB colour, its style.
func ansiHTML(log string) string {
	var out strings.Builder
	for _, l := range joblog.Parse(log) {
		out.WriteString(`<div class="jl-l">`)
		for _, sp := range l {
			open := spanOpen(sp.Style)
			out.WriteString(open + html.EscapeString(sp.Text))
			if open != "" {
				out.WriteString("</span>")
			}
		}
		out.WriteString("</div>")
	}
	return out.String()
}

// spanOpen is the <span> that styles s, "" for the default.
func spanOpen(s joblog.Style) string {
	var cls, css []string
	add := func(c, prop, prefix string) {
		switch {
		case c == "":
		case strings.HasPrefix(c, "#"):
			css = append(css, prop+":"+c)
		default:
			cls = append(cls, prefix+c)
		}
	}
	add(s.FG, "color", "a-c")
	add(s.BG, "background", "a-b")
	for _, f := range []struct {
		on  bool
		cls string
	}{{s.Bold, "a-bold"}, {s.Dim, "a-dim"}, {s.Italic, "a-it"}, {s.Underline, "a-ul"}} {
		if f.on {
			cls = append(cls, f.cls)
		}
	}
	if len(cls) == 0 && len(css) == 0 {
		return ""
	}
	out := "<span"
	if len(cls) > 0 {
		out += ` class="` + strings.Join(cls, " ") + `"`
	}
	if len(css) > 0 {
		out += ` style="` + strings.Join(css, ";") + `"`
	}
	return out + ">"
}
