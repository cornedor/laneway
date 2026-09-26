package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Editing a description as markdown. Only documents made of what markdown
// carries both ways are offered: paragraphs, headings, lists, code, quotes,
// rules, and bold / italic / code / strike / link text. Before one is handed
// out, it is turned into markdown and back and must come out the same, so
// saving an edit never drops a table, a mention or a colour.

// editableBlocks and editableMarks are the node and mark types the round
// trip keeps.
var (
	editableBlocks = []string{"doc", "paragraph", "heading", "bulletList", "orderedList", "listItem",
		"codeBlock", "blockquote", "rule", "text", "hardBreak"}
	editableMarks = []string{"strong", "em", "code", "strike", "link"}
)

// Description fetches the issue's description as its ADF document.
func (c *Client) Description(ctx context.Context, key string) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp struct {
		Fields struct {
			Description json.RawMessage `json:"description"`
		} `json:"fields"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields=description", key, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Fields.Description, nil
}

// SetDescription writes markdown as the issue's description, placeholder
// lines put back from kept; blank clears it.
func (c *Client) SetDescription(ctx context.Context, key, md string, kept []json.RawMessage) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	var doc any
	if strings.TrimSpace(md) != "" {
		doc = MarkdownToADFKept(md, kept)
	}
	body := map[string]any{"fields": map[string]any{"description": doc}}
	if err := c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// Editable is a description as markdown to edit. Blocks markdown can't keep
// (a table, a paragraph with a mention) stand in it as placeholder lines,
// <!-- keep:N … -->, and are put back from Kept[N-1] untouched on save.
type Editable struct {
	Markdown string
	Kept     []json.RawMessage
}

// keepLine matches a placeholder line; its number picks the kept block.
var keepLine = regexp.MustCompile(`^<!-- keep:(\d+)\b.*-->$`)

// keepInline matches an inline placeholder, ⟦3 @Ada Lovelace⟧, standing for
// a kept mention, emoji, date and the like inside editable text.
var keepInline = regexp.MustCompile(`⟦(\d+)[^⟦⟧\n]*⟧`)

// inlineKeepTypes are the leaf inline nodes kept as inline placeholders.
var inlineKeepTypes = []string{"mention", "emoji", "inlineCard", "date", "status", "mediaInline", "placeholder", "inlineExtension"}

// SetComment replaces comment id's body with markdown, placeholder lines
// put back from kept.
func (c *Client) SetComment(ctx context.Context, key, id, md string, kept []json.RawMessage) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	if strings.TrimSpace(md) == "" {
		return fmt.Errorf("jira: empty comment")
	}
	body := map[string]any{"body": MarkdownToADFKept(md, kept)}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/comment/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPut, path, key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// EditableDescription is raw as markdown to edit, or why it can't be.
func EditableDescription(raw json.RawMessage) (Editable, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Editable{}, nil
	}
	var doc adfNode
	var blocks struct {
		Content []json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &doc) != nil || json.Unmarshal(raw, &blocks) != nil || len(blocks.Content) != len(doc.Content) {
		return Editable{}, fmt.Errorf("unreadable description")
	}
	var ed Editable
	var b strings.Builder
	for i, n := range doc.Content {
		if editableBlock(n) {
			writeBlock(&b, n, "")
			continue
		}
		had := len(ed.Kept)
		if sub := keepInlines(n, &ed.Kept); editableBlock(sub) {
			writeBlock(&b, sub, "")
			continue
		}
		ed.Kept = ed.Kept[:had]
		ed.Kept = append(ed.Kept, blocks.Content[i])
		fmt.Fprintf(&b, "<!-- keep:%d %s: move or delete this line -->\n\n", len(ed.Kept), blockName(n))
	}
	ed.Markdown = strings.TrimSpace(b.String())
	// The whole must come back as it was, placeholders and all.
	back, _ := json.Marshal(MarkdownToADFKept(ed.Markdown, ed.Kept))
	var again adfNode
	_ = json.Unmarshal(back, &again)
	if canon(doc) != canon(again) {
		return Editable{}, fmt.Errorf("the description has text markdown would change (like * or `)")
	}
	return ed, nil
}

// keepInlines is n with its unmarked leaf inline nodes (mentions, emoji,
// dates…) as placeholder text, each appended to kept.
func keepInlines(n adfNode, kept *[]json.RawMessage) adfNode {
	if len(n.Content) == 0 || n.Type == "codeBlock" {
		return n
	}
	out := n
	out.Content = make([]adfNode, len(n.Content))
	for i, c := range n.Content {
		if !slices.Contains(inlineKeepTypes, c.Type) || len(c.Content) > 0 || len(c.Marks) > 0 {
			out.Content[i] = keepInlines(c, kept)
			continue
		}
		raw, _ := json.Marshal(map[string]any{"type": c.Type, "attrs": c.Attrs})
		*kept = append(*kept, raw)
		out.Content[i] = adfNode{Type: "text", Text: fmt.Sprintf("⟦%d %s⟧", len(*kept), inlineLabel(c))}
	}
	return out
}

// inlineLabel is what an inline placeholder shows: the mention's name, the
// emoji, the link; stripped of markdown so the text round trips.
func inlineLabel(n adfNode) string {
	label := n.Type
	for _, k := range []string{"text", "shortName", "url", "timestamp"} {
		if v, ok := n.Attrs[k].(string); ok && v != "" {
			label = v
			break
		}
	}
	if n.Type == "date" {
		if ms, err := strconv.ParseInt(label, 10, 64); err == nil {
			label = time.UnixMilli(ms).UTC().Format(time.DateOnly)
		}
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("*~`[]()⟦⟧\n", r) {
			return -1
		}
		return r
	}, label)
}

// restoreInlines puts kept inline nodes back for their placeholders; one
// numbering nothing inline stays text.
func restoreInlines(doc map[string]any, kept []json.RawMessage) {
	if len(kept) == 0 {
		return
	}
	var split func(string) []any
	split = func(text string) []any {
		for _, m := range keepInline.FindAllStringSubmatchIndex(text, -1) {
			n, _ := strconv.Atoi(text[m[2]:m[3]])
			if n < 1 || n > len(kept) {
				continue
			}
			var node struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(kept[n-1], &node) != nil || !slices.Contains(inlineKeepTypes, node.Type) {
				continue
			}
			var out []any
			if m[0] > 0 {
				out = append(out, map[string]any{"type": "text", "text": text[:m[0]]})
			}
			out = append(out, kept[n-1])
			if rest := text[m[1]:]; rest != "" {
				out = append(out, split(rest)...)
			}
			return out
		}
		return []any{map[string]any{"type": "text", "text": text}}
	}
	splitTexts(doc, split)
}

// editableBlock reports whether a top-level block survives markdown and
// back unchanged.
func editableBlock(n adfNode) bool {
	if unsupported(n) != "" {
		return false
	}
	var b strings.Builder
	writeBlock(&b, n, "")
	back, _ := json.Marshal(MarkdownToADF(b.String()))
	var again adfNode
	_ = json.Unmarshal(back, &again)
	return canon(adfNode{Type: "doc", Content: []adfNode{n}}) == canon(again)
}

// blockName says what a kept block is: "table", "paragraph with a mention".
func blockName(n adfNode) string {
	if what := unsupported(adfNode{Type: "doc", Content: n.Content}); what != "" && slices.Contains(editableBlocks, n.Type) {
		return n.Type + " with " + what
	}
	return n.Type
}

// unsupported names the first node or mark outside the editable set.
func unsupported(n adfNode) string {
	if !slices.Contains(editableBlocks, n.Type) {
		return "a " + n.Type
	}
	if n.Type == "orderedList" {
		if o, ok := n.Attrs["order"].(float64); ok && o != 1 {
			return "a list not starting at 1"
		}
	}
	for _, mk := range n.Marks {
		if !slices.Contains(editableMarks, mk.Type) {
			return mk.Type + " text"
		}
	}
	for _, c := range n.Content {
		if what := unsupported(c); what != "" {
			return what
		}
	}
	return ""
}

// canon is a node as the round trip must keep it: types, the attributes
// markdown carries, and text runs with their marks, adjacent runs merged.
func canon(n adfNode) string {
	var b strings.Builder
	b.WriteString(n.Type)
	switch n.Type {
	case "heading":
		level, _ := n.Attrs["level"].(float64)
		b.WriteString(strconv.Itoa(int(level)))
	case "codeBlock":
		lang, _ := n.Attrs["language"].(string)
		b.WriteString(":" + lang)
	}
	b.WriteString("(")
	var run, runMarks string
	flush := func() {
		if run != "" {
			b.WriteString(strconv.Quote(run) + runMarks + ",")
		}
		run, runMarks = "", ""
	}
	for _, c := range n.Content {
		if c.Type != "text" {
			flush()
			b.WriteString(canon(c) + ",")
			continue
		}
		var marks []string
		for _, mk := range c.Marks {
			m := mk.Type
			if href, ok := mk.Attrs["href"].(string); ok {
				m += "=" + href
			}
			marks = append(marks, m)
		}
		slices.Sort(marks)
		ms := "[" + strings.Join(marks, " ") + "]"
		if ms != runMarks {
			flush()
			runMarks = ms
		}
		run += c.Text
	}
	flush()
	b.WriteString(")")
	return b.String()
}

// MarkdownToADF parses the markdown adfToMarkdown writes back into a
// document: the inverse over the editable set.
func MarkdownToADF(md string) map[string]any { return MarkdownToADFKept(md, nil) }

// MarkdownToADFKept is MarkdownToADF with placeholder lines replaced by the
// kept blocks they number; one numbering none is dropped.
func MarkdownToADFKept(md string, kept []json.RawMessage) map[string]any {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	blocks := parseMDBlocks(lines, kept)
	if len(blocks) == 0 {
		blocks = []any{map[string]any{"type": "paragraph", "content": []any{}}}
	}
	doc := map[string]any{"type": "doc", "version": 1, "content": blocks}
	restoreInlines(doc, kept)
	return doc
}

var (
	mdHeading = regexp.MustCompile(`^(#{1,6}) (.*)$`)
	mdBullet  = regexp.MustCompile(`^( *)[-*] (.*)$`)
	mdOrdered = regexp.MustCompile(`^( *)\d+\. (.*)$`)
)

// parseMDBlocks turns lines into block nodes.
func parseMDBlocks(lines []string, kept []json.RawMessage) []any {
	var blocks []any
	for i := 0; i < len(lines); {
		ln := lines[i]
		switch {
		case strings.TrimSpace(ln) == "":
			i++
		case keepLine.MatchString(strings.TrimSpace(ln)):
			n, _ := strconv.Atoi(keepLine.FindStringSubmatch(strings.TrimSpace(ln))[1])
			if n >= 1 && n <= len(kept) {
				blocks = append(blocks, kept[n-1])
			}
			i++
		case strings.HasPrefix(ln, "```"):
			lang := strings.TrimSpace(strings.TrimPrefix(ln, "```"))
			var code []string
			i++
			for i < len(lines) && !strings.HasPrefix(lines[i], "```") {
				code = append(code, lines[i])
				i++
			}
			i++ // the closing fence
			node := map[string]any{"type": "codeBlock", "content": []any{}}
			if text := strings.Join(code, "\n"); text != "" {
				node["content"] = []any{map[string]any{"type": "text", "text": text}}
			}
			if lang != "" {
				node["attrs"] = map[string]any{"language": lang}
			}
			blocks = append(blocks, node)
		case mdHeading.MatchString(ln):
			m := mdHeading.FindStringSubmatch(ln)
			blocks = append(blocks, map[string]any{"type": "heading", "attrs": map[string]any{"level": len(m[1])},
				"content": parseInline(m[2], nil)})
			i++
		case strings.TrimSpace(ln) == "---":
			blocks = append(blocks, map[string]any{"type": "rule"})
			i++
		case strings.HasPrefix(ln, ">"):
			var inner []string
			for i < len(lines) && strings.HasPrefix(lines[i], ">") {
				inner = append(inner, strings.TrimPrefix(strings.TrimPrefix(lines[i], ">"), " "))
				i++
			}
			blocks = append(blocks, map[string]any{"type": "blockquote", "content": parseMDBlocks(inner, kept)})
		case mdBullet.MatchString(ln) || mdOrdered.MatchString(ln):
			var node any
			node, i = parseMDList(lines, i, 0)
			blocks = append(blocks, node)
		default:
			var para []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !startsBlock(lines[i]) {
				para = append(para, lines[i])
				i++
			}
			blocks = append(blocks, paragraph(para))
		}
	}
	return blocks
}

// startsBlock reports whether ln opens a block other than a paragraph.
func startsBlock(ln string) bool {
	return keepLine.MatchString(strings.TrimSpace(ln)) || strings.HasPrefix(ln, "```") || strings.HasPrefix(ln, ">") || mdHeading.MatchString(ln) ||
		strings.TrimSpace(ln) == "---" || mdBullet.MatchString(ln) || mdOrdered.MatchString(ln)
}

// parseMDList reads a list at indent from lines[i], with the lists nested
// under its items, and returns it and the next line.
func parseMDList(lines []string, i, indent int) (any, int) {
	ordered := mdOrdered.MatchString(lines[i])
	re, typ := mdBullet, "bulletList"
	if ordered {
		re, typ = mdOrdered, "orderedList"
	}
	var items []any
	for i < len(lines) {
		m := re.FindStringSubmatch(lines[i])
		if m == nil || len(m[1]) != indent {
			break
		}
		content := []any{paragraph([]string{m[2]})}
		i++
		for i < len(lines) {
			n := mdBullet.FindStringSubmatch(lines[i])
			if n == nil {
				n = mdOrdered.FindStringSubmatch(lines[i])
			}
			if n == nil || len(n[1]) <= indent {
				break
			}
			var sub any
			sub, i = parseMDList(lines, i, len(n[1]))
			content = append(content, sub)
		}
		items = append(items, map[string]any{"type": "listItem", "content": content})
	}
	return map[string]any{"type": typ, "content": items}, i
}

// paragraph is lines as one paragraph, joined by hard breaks.
func paragraph(lines []string) map[string]any {
	var content []any
	for i, ln := range lines {
		if i > 0 {
			content = append(content, map[string]any{"type": "hardBreak"})
		}
		content = append(content, parseInline(ln, nil)...)
	}
	return map[string]any{"type": "paragraph", "content": content}
}

// mdSpans are the inline markers, longest first so ** wins over *.
var mdSpans = []struct{ open, close, mark string }{
	{"**", "**", "strong"}, {"~~", "~~", "strike"}, {"*", "*", "em"}, {"`", "`", "code"},
}

// parseInline turns a line into text nodes carrying marks (plus those of
// the span it sits in).
func parseInline(s string, marks []any) []any {
	var out []any
	text := ""
	emit := func() {
		if text != "" {
			node := map[string]any{"type": "text", "text": text}
			if len(marks) > 0 {
				node["marks"] = slices.Clone(marks)
			}
			out = append(out, node)
			text = ""
		}
	}
	for i := 0; i < len(s); {
		if s[i] == '[' {
			if end := strings.Index(s[i:], "]("); end > 0 {
				if close := strings.IndexByte(s[i+end:], ')'); close > 0 {
					label, href := s[i+1:i+end], s[i+end+2:i+end+close]
					emit()
					link := map[string]any{"type": "link", "attrs": map[string]any{"href": href}}
					out = append(out, parseInline(label, append(slices.Clone(marks), link))...)
					i += end + close + 1
					continue
				}
			}
		}
		matched := false
		for _, sp := range mdSpans {
			if !strings.HasPrefix(s[i:], sp.open) {
				continue
			}
			rest := s[i+len(sp.open):]
			end := strings.Index(rest, sp.close)
			if end <= 0 {
				continue
			}
			emit()
			inner := rest[:end]
			mk := map[string]any{"type": sp.mark}
			if sp.mark == "code" {
				out = append(out, map[string]any{"type": "text", "text": inner, "marks": append(slices.Clone(marks), mk)})
			} else {
				out = append(out, parseInline(inner, append(slices.Clone(marks), mk))...)
			}
			i += len(sp.open) + end + len(sp.close)
			matched = true
			break
		}
		if !matched {
			text += s[i : i+1]
			i++
		}
	}
	emit()
	return out
}
