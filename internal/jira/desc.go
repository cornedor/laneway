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

// SetDescription writes markdown as the issue's description; blank clears it.
func (c *Client) SetDescription(ctx context.Context, key, md string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	var doc any
	if strings.TrimSpace(md) != "" {
		doc = MarkdownToADF(md)
	}
	body := map[string]any{"fields": map[string]any{"description": doc}}
	if err := c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// EditableDescription is raw as markdown to edit, or why it can't be.
func EditableDescription(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("unreadable description")
	}
	if what := unsupported(doc); what != "" {
		return "", fmt.Errorf("the description has %s, which markdown can't keep", what)
	}
	md := adfToMarkdown(raw)
	back, _ := json.Marshal(MarkdownToADF(md))
	var again adfNode
	_ = json.Unmarshal(back, &again)
	if canon(doc) != canon(again) {
		return "", fmt.Errorf("the description has text markdown would change (like * or `)")
	}
	return md, nil
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
func MarkdownToADF(md string) map[string]any {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	blocks := parseMDBlocks(lines)
	if len(blocks) == 0 {
		blocks = []any{map[string]any{"type": "paragraph", "content": []any{}}}
	}
	return map[string]any{"type": "doc", "version": 1, "content": blocks}
}

var (
	mdHeading = regexp.MustCompile(`^(#{1,6}) (.*)$`)
	mdBullet  = regexp.MustCompile(`^( *)[-*] (.*)$`)
	mdOrdered = regexp.MustCompile(`^( *)\d+\. (.*)$`)
)

// parseMDBlocks turns lines into block nodes.
func parseMDBlocks(lines []string) []any {
	var blocks []any
	for i := 0; i < len(lines); {
		ln := lines[i]
		switch {
		case strings.TrimSpace(ln) == "":
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
			blocks = append(blocks, map[string]any{"type": "blockquote", "content": parseMDBlocks(inner)})
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
	return strings.HasPrefix(ln, "```") || strings.HasPrefix(ln, ">") || mdHeading.MatchString(ln) ||
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
