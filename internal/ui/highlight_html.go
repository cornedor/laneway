package ui

import (
	"html"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

// HighlightHTML is lines of the file at path as HTML, a string each, tokens
// in spans of a coarse class (hl-k keyword, hl-t type, hl-n function or
// builtin name, hl-s string, hl-m number, hl-c comment, hl-o operator, hl-g
// tag or attribute) the web's diff view colours from its theme. Past
// diffHighlightMaxLines, or for a file chroma has no lexer for, it is plain.
func HighlightHTML(path string, lines []string) []string {
	out := make([]string, len(lines))
	lexer := lexerForFilename(path)
	if lexer == nil || len(lines) > diffHighlightMaxLines {
		for i, l := range lines {
			out[i] = html.EscapeString(l)
		}
		return out
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(lines, "\n")+"\n")
	if err != nil {
		for i, l := range lines {
			out[i] = html.EscapeString(l)
		}
		return out
	}
	var b strings.Builder
	n := 0
	for tok := it(); tok != chroma.EOF && n < len(lines); tok = it() {
		parts := strings.Split(tok.Value, "\n")
		for j, part := range parts {
			if j > 0 { // a line ends inside this token
				out[n] = b.String()
				b.Reset()
				if n++; n >= len(lines) {
					break
				}
			}
			if part == "" {
				continue
			}
			if cls := tokenClass(tok.Type); cls != "" {
				b.WriteString(`<span class="` + cls + `">` + html.EscapeString(part) + `</span>`)
			} else {
				b.WriteString(html.EscapeString(part))
			}
		}
	}
	if n < len(lines) {
		out[n] = b.String()
	}
	return out
}

// tokenClass is a token type's coarse class, "" for plain text.
func tokenClass(t chroma.TokenType) string {
	switch {
	case t == chroma.KeywordType:
		return "hl-t"
	case t.InCategory(chroma.Keyword):
		return "hl-k"
	case t == chroma.NameFunction, t == chroma.NameBuiltin, t == chroma.NameClass:
		return "hl-n"
	case t == chroma.NameTag, t == chroma.NameAttribute:
		return "hl-g"
	case t.InCategory(chroma.Comment):
		return "hl-c"
	case t.InSubCategory(chroma.LiteralString):
		return "hl-s"
	case t.InSubCategory(chroma.LiteralNumber):
		return "hl-m"
	case t.InCategory(chroma.Operator):
		return "hl-o"
	}
	return ""
}
