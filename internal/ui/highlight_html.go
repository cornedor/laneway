package ui

import (
	"html"
	"strings"
	"unicode/utf16"

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

// CodeSpan is a token of a code block HighlightSpans found: [From, To) in
// UTF-16 units (a browser's string offsets) and its coarse class (as
// HighlightHTML's).
type CodeSpan struct {
	From, To int
	Class    string
}

// HighlightSpans is code in language lang (a fence's or an ADF code
// block's: "go", "typescript") as its tokens of a class; none for a
// language chroma has no lexer for, or past diffHighlightMaxLines.
func HighlightSpans(lang, code string) []CodeSpan {
	lexer := lexerFor(strings.ToLower(strings.TrimSpace(lang)))
	if lexer == nil || strings.Count(code, "\n") > diffHighlightMaxLines {
		return nil
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return nil
	}
	var out []CodeSpan
	at := 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		n := utf16Len(tok.Value)
		if cls := tokenClass(tok.Type); cls != "" && n > 0 && strings.TrimSpace(tok.Value) != "" {
			if k := len(out) - 1; k >= 0 && out[k].Class == cls && out[k].To == at {
				out[k].To += n
			} else {
				out = append(out, CodeSpan{at, at + n, cls})
			}
		}
		at += n
	}
	return out
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
