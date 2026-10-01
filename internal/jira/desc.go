package jira

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/cornedor/laneway/internal/emoji"
)

// Editing a description as markdown. Only what markdown carries both ways
// is offered as text: paragraphs, headings, lists, tasks, decisions, code,
// quotes, rules, panels, expands, plain tables, and bold / italic / code /
// strike / underline / sub / sup / colour / link text. Before a document
// is handed out, it is turned into markdown and back and must come out the
// same, so saving an edit never drops a mention or a merged cell.

// editableBlocks and editableMarks are the node and mark types the round
// trip keeps.
var (
	editableBlocks = []string{"doc", "paragraph", "heading", "bulletList", "orderedList", "listItem",
		"codeBlock", "blockquote", "rule", "text", "hardBreak", "emoji", "panel", "expand",
		"table", "tableRow", "tableHeader", "tableCell", "taskList", "taskItem", "decisionList", "decisionItem",
		"mention", "status", "date", "inlineCard", "blockCard"}
	editableMarks = []string{"strong", "em", "code", "strike", "link", "underline", "subsup", "textColor", "backgroundColor"}
)

// containerTypes are blocks whose own attributes markdown can't carry but
// whose content it can: they edit as their blocks between
// <!-- block:N … --> and <!-- /block -->, the node itself kept.
var containerTypes = []string{"panel", "expand", "nestedExpand", "blockquote", "bodiedSyncBlock",
	"layoutSection", "layoutColumn", "bodiedExtension"}

// panelTypes are the panels <!-- panel:type --> names.
var panelTypes = []string{"info", "note", "success", "warning", "error"}

// cssColor is a colour a <span style> carries: #rgb or #rrggbb.
var cssColor = regexp.MustCompile(`^#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?$`)

// Description fetches the issue's description as its ADF document.
func (c *Client) Description(ctx context.Context, key string) (json.RawMessage, error) {
	return c.RawField(ctx, key, "description")
}

// RawField fetches one field of the issue as Jira has it: a rich-text one
// as its ADF document, null when empty.
func (c *Client) RawField(ctx context.Context, key, id string) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"?fields="+url.QueryEscape(id), key, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Fields[id], nil
}

// SetDescription writes markdown as the issue's description, placeholder
// lines put back from kept; blank clears it.
func (c *Client) SetDescription(ctx context.Context, key, md string, kept []json.RawMessage) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	var doc any
	if strings.TrimSpace(md) != "" {
		doc = MarkdownToADFKept(c.EmbedImages(ctx, md), kept)
	}
	body := map[string]any{"fields": map[string]any{"description": doc}}
	if err := c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// ToggleTask checks or unchecks the description's nth action item (from
// 1, in reading order, those in tables not counted, as the panel draws
// them), leaving the rest of the document as it is. It fails, writing
// nothing, when the description no longer has total items or the nth
// isn't in the state the panel showed.
func (c *Client) ToggleTask(ctx context.Context, key string, n, total int, done bool) error {
	raw, err := c.Description(ctx, key)
	if err != nil {
		return err
	}
	doc, err := toggleTask(raw, n, total, done)
	if err != nil {
		return err
	}
	body := map[string]any{"fields": map[string]any{"description": doc}}
	if err := c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// toggleTask is raw with its nth task item set done or not; see ToggleTask.
func toggleTask(raw json.RawMessage, n, total int, done bool) (any, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unreadable description")
	}
	var items []map[string]any
	var walk func(any)
	walk = func(v any) {
		node, ok := v.(map[string]any)
		if !ok || node["type"] == "table" {
			return
		}
		if node["type"] == "taskItem" {
			items = append(items, node)
		}
		content, _ := node["content"].([]any)
		for _, c := range content {
			walk(c)
		}
	}
	walk(doc)
	changed := fmt.Errorf("the description changed since it was shown; refresh")
	if len(items) != total || n < 1 || n > total {
		return nil, changed
	}
	attrs, _ := items[n-1]["attrs"].(map[string]any)
	if attrs == nil || (attrs["state"] == "DONE") == done {
		return nil, changed
	}
	attrs["state"] = map[bool]string{true: "DONE", false: "TODO"}[done]
	return doc, nil
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
	body := map[string]any{"body": MarkdownToADFKept(c.EmbedImages(ctx, md), kept)}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/comment/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPut, path, key, body, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// DeleteComment deletes comment id on key.
func (c *Client) DeleteComment(ctx context.Context, key, id string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/comment/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodDelete, path, key, nil, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// DeleteIssue deletes key, and its subtasks with it when subtasks is set;
// Jira refuses an issue with subtasks otherwise.
func (c *Client) DeleteIssue(ctx context.Context, key string, subtasks bool) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?deleteSubtasks=" + strconv.FormatBool(subtasks)
	if err := c.do(ctx, http.MethodDelete, path, key, nil, nil); err != nil {
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
	if json.Unmarshal(raw, &doc) != nil {
		return Editable{}, fmt.Errorf("unreadable description")
	}
	raws := childRaws(raw)
	if len(raws) != len(doc.Content) {
		return Editable{}, fmt.Errorf("unreadable description")
	}
	var ed Editable
	var b strings.Builder
	vocab := vocabulary(raw)
	editBlocks(&b, doc.Content, raws, &ed.Kept, vocab)
	ed.Kept = append(ed.Kept, vocab...)
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

// editBlocks writes nodes as markdown to edit. A block markdown can't
// keep stands as a placeholder line, raws[i] appended to kept; a
// container of such blocks edits as its content, the node kept.
func editBlocks(b *strings.Builder, nodes []adfNode, raws []json.RawMessage, kept *[]json.RawMessage, vocab []json.RawMessage) {
	for i, n := range nodes {
		if editableBlock(n, vocab) {
			writeBlock(b, escapeTexts(n), "")
			continue
		}
		had := len(*kept)
		// sub's ⟦N⟧ point into kept, the vocabulary after it as on save.
		if sub := keepInlines(n, kept); editableWith(n, sub, append(slices.Clone(*kept), vocab...)) {
			writeBlock(b, escapeTexts(sub), "")
			continue
		}
		*kept = (*kept)[:had]
		inner := childRaws(raws[i])
		if slices.Contains(containerTypes, n.Type) && len(n.Content) > 0 && len(inner) == len(n.Content) {
			*kept = append(*kept, shell(raws[i]))
			fmt.Fprintf(b, "<!-- block:%d %s -->\n\n", len(*kept), blockLabel(n.Type))
			editBlocks(b, n.Content, inner, kept, vocab)
			b.WriteString(blockClose + "\n\n")
			continue
		}
		*kept = append(*kept, raws[i])
		fmt.Fprintf(b, "<!-- keep:%d %s: move or delete this line -->\n\n", len(*kept), blockName(n))
	}
}

// vocabulary is the document's mentions and emoji, one of each: kept
// with the markdown, they turn "@Name" and ":name:" back into the people
// and emoji they were (see MarkdownToADFKept). A mention picked while
// editing is added to them the same way.
func vocabulary(raw json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	seen := map[string]bool{}
	var walk func(json.RawMessage)
	walk = func(r json.RawMessage) {
		var n struct {
			Type    string            `json:"type"`
			Attrs   map[string]any    `json:"attrs"`
			Content []json.RawMessage `json:"content"`
		}
		if json.Unmarshal(r, &n) != nil {
			return
		}
		key := ""
		switch n.Type {
		case "mention":
			id, _ := n.Attrs["id"].(string)
			text, _ := n.Attrs["text"].(string)
			key = "@" + id + text
		case "emoji":
			short, _ := n.Attrs["shortName"].(string)
			key = short
		}
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, MentionNode(n.Type, n.Attrs))
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(raw)
	return out
}

// MentionNode is an inline node of typ with attrs, as JSON to keep.
func MentionNode(typ string, attrs map[string]any) json.RawMessage {
	out, _ := json.Marshal(map[string]any{"type": typ, "attrs": attrs})
	return out
}

// childRaws is a node's content, each child as its own JSON.
func childRaws(raw json.RawMessage) []json.RawMessage {
	var n struct {
		Content []json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(raw, &n)
	return n.Content
}

// shell is a node's JSON without its content.
func shell(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	delete(m, "content")
	out, _ := json.Marshal(m)
	return out
}

// blockLabel names a kept container in its marker line.
func blockLabel(typ string) string {
	switch typ {
	case "bodiedSyncBlock":
		return "synced block"
	case "layoutSection":
		return "columns"
	case "layoutColumn":
		return "column"
	case "bodiedExtension":
		return "macro"
	case "panel":
		return "custom panel"
	}
	return typ
}

// Panels and expands edit as their blocks between two marker lines:
// <!-- panel:success --> … <!-- /panel -->, <!-- expand: Title --> …
// <!-- /expand -->.
var (
	panelOpen   = regexp.MustCompile(`^<!-- panel:([a-z]+) -->$`)
	panelClose  = "<!-- /panel -->"
	expandOpen  = regexp.MustCompile(`^<!-- expand(?:: (.*))? -->$`)
	expandClose = "<!-- /expand -->"
	blockOpen   = regexp.MustCompile(`^<!-- block:(\d+)\b.*-->$`)
	blockClose  = "<!-- /block -->"
)

// opensContainer and closesContainer report whether a trimmed line is a
// container's opening or closing marker.
func opensContainer(ln string) bool {
	return panelOpen.MatchString(ln) || expandOpen.MatchString(ln) || blockOpen.MatchString(ln)
}

func closesContainer(ln string) bool {
	return ln == panelClose || ln == expandClose || ln == blockClose
}

// containerEnd is the index of the marker closing the container opened at
// lines[i], nested containers and code fences skipped; len(lines) when
// none does.
func containerEnd(lines []string, i int) int {
	depth := 0
	fence := false
	for j := i; j < len(lines); j++ {
		ln := strings.TrimSpace(lines[j])
		switch {
		case strings.HasPrefix(lines[j], "```"):
			fence = !fence
		case fence:
		case opensContainer(ln):
			depth++
		case closesContainer(ln):
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return len(lines)
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
func editableBlock(n adfNode, vocab []json.RawMessage) bool {
	if unsupported(n) != "" {
		return false
	}
	var b strings.Builder
	writeBlock(&b, escapeTexts(n), "")
	back, _ := json.Marshal(MarkdownToADFKept(b.String(), vocab))
	var again adfNode
	_ = json.Unmarshal(back, &again)
	return canon(adfNode{Type: "doc", Content: []adfNode{n}}) == canon(again)
}

// editableWith: sub, n with inline placeholders, written as markdown and
// read back with list (the kept nodes they name, then the vocabulary) is n.
func editableWith(n, sub adfNode, list []json.RawMessage) bool {
	if unsupported(sub) != "" {
		return false
	}
	var b strings.Builder
	writeBlock(&b, escapeTexts(sub), "")
	back, _ := json.Marshal(MarkdownToADFKept(b.String(), list))
	var again adfNode
	_ = json.Unmarshal(back, &again)
	return canon(adfNode{Type: "doc", Content: []adfNode{n}}) == canon(again)
}

// escapeTexts is n with a backslash before each character of its text
// that markdown would read as markup: *, ~, `, [, \ and a tag's < anywhere,
// and what would start a block (#, >, -, +, <, |, 1.) at a line's start. Text a comment
// posted as plain words ("**not bold**") so edits as it was. Code keeps
// its text as is.
func escapeTexts(n adfNode) adfNode {
	if n.Type == "codeBlock" || len(n.Content) == 0 {
		return n
	}
	out := n
	out.Content = make([]adfNode, len(n.Content))
	start := true // at a line's start: the block's, or after a hard break
	for i, c := range n.Content {
		switch {
		case c.Type == "text" && !slices.ContainsFunc(c.Marks, func(mk adfMark) bool { return mk.Type == "code" }):
			c.Text = escapeMD(c.Text, start)
		case c.Type != "text":
			c = escapeTexts(c)
		}
		out.Content[i] = c
		start = c.Type == "hardBreak" || (start && c.Type == "text" && c.Text == "")
	}
	return out
}

// mdBlockStart finds what would make a line a heading, quote, list or keep
// placeholder.
var mdBlockStart = regexp.MustCompile(`^(#|>|-|\+|<|\||[0-9]+\.)`)

// mdTagStart is an inline tag parseInline reads, at a string's start.
var mdTagStart = regexp.MustCompile(`^(?:</?(?:u|sub|sup|span|status|date)[\s>]|<https?://)`)

// escapeMD escapes s's markup characters; start is whether it begins a
// line.
func escapeMD(s string, start bool) string {
	var b strings.Builder
	if m := mdBlockStart.FindString(s); start && m != "" {
		b.WriteString(m[:len(m)-1] + "\\" + m[len(m)-1:])
		s = s[len(m):]
	}
	for i, r := range s {
		atName := r == '@' && (i == 0 || !isWordByte(s[i-1])) && i+1 < len(s) && unicode.IsLetter(rune(s[i+1]))
		if strings.ContainsRune("\\*~`[", r) || r == '<' && mdTagStart.MatchString(s[i:]) || atName {
			b.WriteByte('\\')
		}
		if _, _, ok := emojiAt(s, i); ok { // a typed shortcode stays text
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mdEmoji is a :shortcode: at a string's start.
var mdEmoji = regexp.MustCompile(`^:[a-zA-Z0-9_+\-]+:`)

// emojiAt is the shortcode of an emoji the table knows at s[i:], and its
// length: one standing alone, not in a time (10:30:00), a word or `quotes`.
func emojiAt(s string, i int) (short string, n int, ok bool) {
	if s[i] != ':' || i > 0 && isWordByte(s[i-1]) {
		return "", 0, false
	}
	short = mdEmoji.FindString(s[i:])
	if short == "" || i+len(short) < len(s) && isWordByte(s[i+len(short)]) || emoji.Glyph(short[1:len(short)-1]) == "" {
		return "", 0, false
	}
	return short, len(short), true
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '`'
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
	switch n.Type {
	case "panel":
		typ, _ := n.Attrs["panelType"].(string)
		if !slices.Contains(panelTypes, typ) || n.Attrs["panelIcon"] != nil || n.Attrs["panelColor"] != nil {
			return "a custom panel"
		}
	case "expand":
		if title, _ := n.Attrs["title"].(string); strings.Contains(title, "-->") || strings.Contains(title, "\n") {
			return "an expand title markdown can't hold"
		}
	case "decisionItem":
		if n.Attrs["state"] != "DECIDED" {
			return "an undecided decision"
		}
	case "mention":
		if text, _ := n.Attrs["text"].(string); strings.TrimPrefix(text, "@") == "" {
			return "a mention"
		}
	case "status":
		if c, _ := n.Attrs["color"].(string); !statusColor.MatchString(c) {
			return "a status"
		}
	case "date":
		if _, ok := adfMillis(n.Attrs["timestamp"]); !ok {
			return "a date"
		}
	case "inlineCard":
		if u, _ := n.Attrs["url"].(string); u == "" {
			return "a smart link"
		}
	case "blockCard":
		if u, _ := n.Attrs["url"].(string); u == "" || n.Attrs["datasource"] != nil {
			return "a card with a view"
		}
	case "table":
		if what := tableUnsupported(n); what != "" {
			return what
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

// tableUnsupported names what a pipe table can't hold of table n: a
// number column, a header column, colours, merged cells, a cell of more
// than one line.
func tableUnsupported(n adfNode) string {
	if n.Attrs["isNumberColumnEnabled"] == true {
		return "a numbered table"
	}
	if len(n.Content) == 0 {
		return "an empty table"
	}
	cols := len(n.Content[0].Content)
	if cols == 0 {
		return "an empty table"
	}
	for _, row := range n.Content {
		if len(row.Content) != cols {
			return "a table with uneven rows"
		}
		for _, cell := range row.Content {
			if bg, _ := cell.Attrs["background"].(string); bg != "" && !cssColor.MatchString(bg) {
				return "a coloured table"
			}
			for _, k := range []string{"colspan", "rowspan"} {
				if v, ok := cell.Attrs[k].(float64); ok && v > 1 {
					return "a table with merged cells"
				}
			}
			lines := 0
			for _, c := range cell.Content {
				if c.Type != "paragraph" || slices.ContainsFunc(c.Content, func(x adfNode) bool { return x.Type == "hardBreak" }) {
					return "a table cell with more than a line"
				}
				if len(trimEnds(c.Content)) > 0 {
					lines++
				}
			}
			if lines > 1 {
				return "a table cell with more than a line"
			}
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
	case "panel":
		typ, _ := n.Attrs["panelType"].(string)
		b.WriteString(":" + typ)
	case "emoji":
		short, _ := n.Attrs["shortName"].(string)
		b.WriteString(short)
	case "orderedList":
		b.WriteString(strconv.Itoa(listOrder(n)))
	case "taskItem", "decisionItem":
		state, _ := n.Attrs["state"].(string)
		b.WriteString(":" + state)
	case "expand":
		title, _ := n.Attrs["title"].(string)
		b.WriteString(":" + strconv.Quote(title))
	case "mention":
		id, _ := n.Attrs["id"].(string)
		b.WriteString(":" + id)
	case "status", "date", "inlineCard": // as written: a status upper case unless mixed
		b.WriteString(":" + strconv.Quote(inline([]adfNode{n})))
	case "blockCard":
		u, _ := n.Attrs["url"].(string)
		b.WriteString(":" + mdHref(u))
	case "tableCell", "tableHeader":
		bg, _ := n.Attrs["background"].(string)
		b.WriteString(":" + strings.ToLower(bg))
		b.WriteString(tableAttrs(n, "colwidth", "colspan", "rowspan"))
	case "table":
		b.WriteString(tableAttrs(n, "layout", "width", "isNumberColumnEnabled"))
	}
	b.WriteString("(")
	content := n.Content
	if slices.Contains([]string{"paragraph", "heading", "taskItem", "decisionItem"}, n.Type) {
		content = trimEnds(content)
	}
	var run, runMarks string
	flush := func() {
		if run != "" {
			b.WriteString(strconv.Quote(run) + runMarks + ",")
		}
		run, runMarks = "", ""
	}
	for _, c := range content {
		if c.Type == "paragraph" && len(trimEnds(c.Content)) == 0 {
			continue // an empty paragraph: markdown has none, Jira shows a gap
		}
		if c.Type != "text" {
			flush()
			b.WriteString(canon(c) + ",")
			continue
		}
		code := slices.ContainsFunc(c.Marks, func(mk adfMark) bool { return mk.Type == "code" })
		if run != "" && strings.TrimSpace(c.Text) == "" && !code {
			run += c.Text // marks on spaces write as none; they show as none
			continue
		}
		var marks []string
		for _, mk := range c.Marks {
			m := mk.Type
			if href, ok := mk.Attrs["href"].(string); ok && mk.Type == "link" {
				m += "=" + mdHref(href)
				if title, _ := mk.Attrs["title"].(string); title != "" {
					m += " title=" + strconv.Quote(title) // markdown writes none: such a link is kept
				}
			} else {
				for _, k := range slices.Sorted(maps.Keys(mk.Attrs)) {
					m += fmt.Sprintf(" %s=%v", k, mk.Attrs[k])
				}
			}
			marks = append(marks, m)
		}
		slices.Sort(marks)
		ms := "[" + strings.Join(marks, ";") + "]"
		if strings.TrimSpace(c.Text) == "" && !code {
			ms = "[]"
		}
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

// trimEnds is a text block's content without the spaces and line breaks
// that begin and end it: markdown drops them, and Jira shows none.
func trimEnds(content []adfNode) []adfNode {
	out := slices.Clone(content)
	for len(out) > 0 {
		switch f := &out[0]; {
		case f.Type == "hardBreak" || f.Type == "text" && strings.TrimLeft(f.Text, " ") == "":
			out = out[1:]
			continue
		case f.Type == "text":
			f.Text = strings.TrimLeft(f.Text, " ")
		}
		break
	}
	for len(out) > 0 {
		switch l := &out[len(out)-1]; {
		case l.Type == "hardBreak" || l.Type == "text" && strings.TrimRight(l.Text, " ") == "":
			out = out[:len(out)-1]
			continue
		case l.Type == "text":
			l.Text = strings.TrimRight(l.Text, " ")
		}
		break
	}
	return out
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
	applyVocab(doc, kept)
	stripTexts(doc, escapedAt, "")
	return doc
}

// escapedAt follows an escaped "@" until the vocabulary has passed, so
// "\@Ann" stays text.
const escapedAt = "\uE000"

// applyVocab makes each "@Name" and ":name:" of the kept vocabulary (see
// vocabulary) in unmarked text the mention or emoji it names, a known
// emoji written as its shortcode taking the kept node's attributes too.
func applyVocab(doc map[string]any, kept []json.RawMessage) {
	type word struct {
		text string
		node json.RawMessage
	}
	var words []word
	emojis := map[string]map[string]any{}
	for _, k := range kept {
		var n struct {
			Type  string         `json:"type"`
			Attrs map[string]any `json:"attrs"`
		}
		if json.Unmarshal(k, &n) != nil {
			continue
		}
		switch n.Type {
		case "mention":
			if text, _ := n.Attrs["text"].(string); strings.TrimPrefix(text, "@") != "" {
				words = append(words, word{"@" + strings.TrimPrefix(text, "@"), k})
			}
		case "emoji":
			if short, _ := n.Attrs["shortName"].(string); mdEmoji.MatchString(short) {
				words = append(words, word{short, k})
				emojis[short] = n.Attrs
			}
		}
	}
	if len(words) == 0 {
		return
	}
	slices.SortStableFunc(words, func(a, b word) int { return len(b.text) - len(a.text) })
	var split func(string) []any
	split = func(text string) []any {
		for i := 0; i < len(text); i++ {
			for _, w := range words {
				end := i + len(w.text)
				if !strings.HasPrefix(text[i:], w.text) || i > 0 && isWordByte(text[i-1]) || end < len(text) && isWordByte(text[end]) {
					continue
				}
				var out []any
				if i > 0 {
					out = append(out, map[string]any{"type": "text", "text": text[:i]})
				}
				out = append(out, w.node)
				if end < len(text) {
					out = append(out, split(text[end:])...)
				}
				return out
			}
		}
		return []any{map[string]any{"type": "text", "text": text}}
	}
	splitTexts(doc, split)
	var walk func(any)
	walk = func(v any) {
		node, ok := v.(map[string]any)
		if !ok {
			return
		}
		if attrs, ok := node["attrs"].(map[string]any); ok && node["type"] == "emoji" {
			if short, _ := attrs["shortName"].(string); emojis[short] != nil {
				node["attrs"] = emojis[short]
			}
		}
		content, _ := node["content"].([]any)
		for _, c := range content {
			walk(c)
		}
	}
	walk(doc)
}

// stripTexts replaces old by new in every text under node.
func stripTexts(node map[string]any, old, new string) {
	if t, ok := node["text"].(string); ok {
		node["text"] = strings.ReplaceAll(t, old, new)
	}
	content, _ := node["content"].([]any)
	for _, c := range content {
		if cm, ok := c.(map[string]any); ok {
			stripTexts(cm, old, new)
		}
	}
}

var (
	mdHeading  = regexp.MustCompile(`^(#{1,6}) (.*)$`)
	mdBullet   = regexp.MustCompile(`^( *)[-*] (.*)$`)
	mdOrdered  = regexp.MustCompile(`^( *)\d+\. (.*)$`)
	mdTask     = regexp.MustCompile(`^( *)[-*] \[([ xX])\](?: (.*))?$`)
	mdDecision = regexp.MustCompile(`^( *)<>(?: (.*))?$`)
	mdTableSep = regexp.MustCompile(`^\|?(?:\s*:?-+:?\s*\|)*\s*:?-+:?\s*\|?$`)
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
		case panelOpen.MatchString(strings.TrimSpace(ln)):
			typ := panelOpen.FindStringSubmatch(strings.TrimSpace(ln))[1]
			end := containerEnd(lines, i)
			blocks = append(blocks, map[string]any{"type": "panel", "attrs": map[string]any{"panelType": typ}, "content": parseMDBlocks(lines[i+1:end], kept)})
			i = end + 1
		case expandOpen.MatchString(strings.TrimSpace(ln)):
			node := map[string]any{"type": "expand"}
			if title := expandOpen.FindStringSubmatch(strings.TrimSpace(ln))[1]; title != "" {
				node["attrs"] = map[string]any{"title": title}
			}
			end := containerEnd(lines, i)
			node["content"] = parseMDBlocks(lines[i+1:end], kept)
			blocks = append(blocks, node)
			i = end + 1
		case blockOpen.MatchString(strings.TrimSpace(ln)):
			n, _ := strconv.Atoi(blockOpen.FindStringSubmatch(strings.TrimSpace(ln))[1])
			end := containerEnd(lines, i)
			inner := parseMDBlocks(lines[i+1:end], kept)
			var node map[string]any
			if n >= 1 && n <= len(kept) && json.Unmarshal(kept[n-1], &node) == nil && node != nil {
				node["content"] = inner
				blocks = append(blocks, node)
			} else { // its node unknown: the content stands alone
				blocks = append(blocks, inner...)
			}
			i = end + 1
		case closesContainer(strings.TrimSpace(ln)): // one left alone
			i++
		case mdCard.MatchString(strings.TrimSpace(ln)):
			blocks = append(blocks, map[string]any{"type": "blockCard", "attrs": map[string]any{"url": mdCard.FindStringSubmatch(strings.TrimSpace(ln))[1]}})
			i++
		case mdMedia.MatchString(strings.TrimSpace(ln)):
			m := mdMedia.FindStringSubmatch(strings.TrimSpace(ln))
			blocks = append(blocks, map[string]any{"type": "mediaSingle", "attrs": map[string]any{"layout": "center"}, "content": []any{
				map[string]any{"type": "media", "attrs": map[string]any{"type": "file", "id": m[2], "collection": "", "alt": m[1]}}}})
			i++
		case tableAt(lines, i):
			var node any
			node, i = parseMDTable(lines, i)
			blocks = append(blocks, node)
		case mdDecision.MatchString(ln):
			var items []any
			for ; i < len(lines) && mdDecision.MatchString(lines[i]); i++ {
				items = append(items, map[string]any{"type": "decisionItem", "attrs": map[string]any{"localId": localID(), "state": "DECIDED"},
					"content": inlineContent(mdDecision.FindStringSubmatch(lines[i])[2])})
			}
			blocks = append(blocks, map[string]any{"type": "decisionList", "attrs": map[string]any{"localId": localID()}, "content": items})
		case mdTask.MatchString(ln):
			var node any
			node, i = parseMDTasks(lines, i, len(mdTask.FindStringSubmatch(ln)[1]))
			blocks = append(blocks, node)
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
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !startsBlock(lines[i]) && !tableAt(lines, i) {
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
	t := strings.TrimSpace(ln)
	return keepLine.MatchString(t) || mdCard.MatchString(t) || mdMedia.MatchString(t) || opensContainer(t) || closesContainer(t) || strings.HasPrefix(ln, "```") || strings.HasPrefix(ln, ">") || mdHeading.MatchString(ln) ||
		t == "---" || mdBullet.MatchString(ln) || mdOrdered.MatchString(ln) || mdDecision.MatchString(ln)
}

// mdCard is a card line, <!-- card: https://… -->.
var mdCard = regexp.MustCompile(`^<!-- card: (\S+) -->$`)

// mdMedia is an image line EmbedImages pointed at a media file.
var mdMedia = regexp.MustCompile(`^!\[([^\]]*)\]\(` + mediaScheme + `([0-9a-fA-F-]{36})\)$`)

// tableAt reports whether a pipe table starts at lines[i]: a row, then a
// separator row.
func tableAt(lines []string, i int) bool {
	return i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") &&
		strings.Contains(lines[i+1], "-") && mdTableSep.MatchString(strings.TrimSpace(lines[i+1]))
}

// parseMDTable reads the pipe table at lines[i] and returns it and the
// next line. An empty header row stands for a table without one.
func parseMDTable(lines []string, i int) (any, int) {
	header := splitCells(lines[i])
	var body [][]string
	for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
		body = append(body, splitCells(lines[i]))
	}
	cols := len(header)
	for _, r := range body {
		cols = max(cols, len(r))
	}
	row := func(cells []string, typ string) any {
		var out []any
		for c := range cols {
			text := ""
			if c < len(cells) {
				text = cells[c]
			}
			cell := map[string]any{"type": typ}
			for { // its markers: a background, a header cell
				if m := mdCellBG.FindStringSubmatch(text); m != nil {
					cell["attrs"] = map[string]any{"background": m[1]}
					text = text[len(m[0]):]
				} else if rest, ok := strings.CutPrefix(text, "<!-- th -->"); ok {
					cell["type"], text = "tableHeader", strings.TrimSpace(rest)
				} else {
					break
				}
			}
			cell["content"] = []any{map[string]any{"type": "paragraph", "content": inlineContent(text)}}
			out = append(out, cell)
		}
		return map[string]any{"type": "tableRow", "content": out}
	}
	var rows []any
	if slices.ContainsFunc(header, func(c string) bool { return c != "" }) || len(body) == 0 {
		rows = append(rows, row(header, "tableHeader"))
	}
	for _, r := range body {
		rows = append(rows, row(r, "tableCell"))
	}
	return map[string]any{"type": "table", "content": rows}, i
}

// mdCellBG is a cell's background marker, <!-- bg:#deebff -->.
var mdCellBG = regexp.MustCompile(`^<!-- bg:(#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?) -->\s*`)

// splitCells is a table row's cells, trimmed, on its unescaped pipes; an
// escaped pipe is kept as a pipe.
func splitCells(ln string) []string {
	ln = strings.TrimPrefix(strings.TrimSpace(ln), "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(ln); i++ {
		switch {
		case ln[i] == '\\' && i+1 < len(ln) && ln[i+1] == '|':
			cur.WriteByte('|')
			i++
		case ln[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ln[i])
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		cells = append(cells, rest)
	}
	return cells
}

// parseMDTasks reads a task list at indent from lines[i], a deeper one
// nested in it, and returns it and the next line.
func parseMDTasks(lines []string, i, indent int) (any, int) {
	var items []any
	for i < len(lines) {
		m := mdTask.FindStringSubmatch(lines[i])
		if m == nil || len(m[1]) < indent {
			break
		}
		if len(m[1]) > indent {
			var sub any
			sub, i = parseMDTasks(lines, i, len(m[1]))
			items = append(items, sub)
			continue
		}
		state := "TODO"
		if m[2] != " " {
			state = "DONE"
		}
		items = append(items, map[string]any{"type": "taskItem", "attrs": map[string]any{"localId": localID(), "state": state}, "content": inlineContent(m[3])})
		i++
	}
	return map[string]any{"type": "taskList", "attrs": map[string]any{"localId": localID()}, "content": items}, i
}

// localID is a fresh id for a task or decision, which Jira requires.
func localID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// inlineContent is s's inline nodes, never nil: Jira refuses a null.
func inlineContent(s string) []any {
	if out := parseInline(s, nil); out != nil {
		return out
	}
	return []any{}
}

// parseMDList reads a list at indent from lines[i], with the lists nested
// under its items, and returns it and the next line.
func parseMDList(lines []string, i, indent int) (any, int) {
	ordered := mdOrdered.MatchString(lines[i])
	re, typ := mdBullet, "bulletList"
	if ordered {
		re, typ = mdOrdered, "orderedList"
	}
	node := map[string]any{"type": typ}
	if n, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(lines[i], ".", 2)[0])); ordered && n != 1 {
		node["attrs"] = map[string]any{"order": n}
	}
	var items []any
	for i < len(lines) {
		m := re.FindStringSubmatch(lines[i])
		if m == nil || len(m[1]) != indent || mdTask.MatchString(lines[i]) {
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
			if mdTask.MatchString(lines[i]) {
				sub, i = parseMDTasks(lines, i, len(n[1]))
			} else {
				sub, i = parseMDList(lines, i, len(n[1]))
			}
			content = append(content, sub)
		}
		items = append(items, map[string]any{"type": "listItem", "content": content})
	}
	node["content"] = items
	return node, i
}

// paragraph is lines as one paragraph, joined by hard breaks.
func paragraph(lines []string) map[string]any {
	content := []any{}
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

// mdTagOpen is an opening tag for a mark markdown lacks: <u>, <sub>,
// <sup>, <span style="color:#rrggbb">, <span style="background-color:…">.
var mdTagOpen = regexp.MustCompile(`^<(?:(u|sub|sup)|(span) style="(color|background-color):\s*(#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?);?")>`)

// mdStatus is a status lozenge: <status color="green">DONE</status>.
var mdStatus = regexp.MustCompile(`^<status color="(neutral|purple|blue|red|yellow|green)">([^<\n]+)</status>`)

// mdDate is a date, <date>2026-09-30</date>; mdAutolink a smart link,
// <https://…>.
var (
	mdDate     = regexp.MustCompile(`^<date>(\d{4}-\d{2}-\d{2})</date>`)
	mdAutolink = regexp.MustCompile(`^<(https?://[^\s<>]+)>`)
)

// tagMark is the mark an mdTagOpen match stands for.
func tagMark(tag []string) map[string]any {
	switch {
	case tag[1] == "u":
		return map[string]any{"type": "underline"}
	case tag[1] != "":
		return map[string]any{"type": "subsup", "attrs": map[string]any{"type": tag[1]}}
	case tag[3] == "color":
		return map[string]any{"type": "textColor", "attrs": map[string]any{"color": tag[4]}}
	}
	return map[string]any{"type": "backgroundColor", "attrs": map[string]any{"color": tag[4]}}
}

// closingTag is the index in s of the </name> closing a tag just opened,
// nested ones of the same name skipped; -1 when none does.
func closingTag(s, name string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case strings.HasPrefix(s[i:], "</"+name+">"):
			if depth == 0 {
				return i
			}
			depth--
		case strings.HasPrefix(s[i:], "<"+name+">") || strings.HasPrefix(s[i:], "<"+name+" "):
			depth++
		}
	}
	return -1
}

// isASCIIPunct is whether c may be backslash-escaped, as in CommonMark.
func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
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
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) { // an escape: the character itself
			text += s[i+1 : i+2]
			if s[i+1] == '@' {
				text += escapedAt
			}
			i += 2
			continue
		}
		if m := mdDate.FindStringSubmatch(s[i:]); m != nil {
			if day, err := time.Parse(time.DateOnly, m[1]); err == nil {
				emit()
				out = append(out, map[string]any{"type": "date", "attrs": map[string]any{"timestamp": strconv.FormatInt(day.UnixMilli(), 10)}})
				i += len(m[0])
				continue
			}
		}
		if m := mdAutolink.FindStringSubmatch(s[i:]); m != nil { // a smart link takes no marks
			emit()
			out = append(out, map[string]any{"type": "inlineCard", "attrs": map[string]any{"url": m[1]}})
			i += len(m[0])
			continue
		}
		if s[i] == '[' {
			if end := strings.Index(s[i:], "]("); end > 0 {
				if close := strings.IndexByte(s[i+end:], ')'); close > 0 {
					label, href := s[i+1:i+end], s[i+end+2:i+end+close]
					emit()
					attrs := map[string]any{"href": href}
					if h, title, ok := strings.Cut(href, ` "`); ok && strings.HasSuffix(title, `"`) && !strings.Contains(h, " ") {
						attrs["href"], attrs["title"] = h, strings.TrimSuffix(title, `"`) // [text](url "title")
					}
					link := map[string]any{"type": "link", "attrs": attrs}
					out = append(out, parseInline(label, append(slices.Clone(marks), link))...)
					i += end + close + 1
					continue
				}
			}
		}
		if m := mdStatus.FindStringSubmatch(s[i:]); m != nil { // a status takes no marks
			emit()
			out = append(out, map[string]any{"type": "status", "attrs": map[string]any{"text": m[2], "color": m[1], "style": "mixedCase", "localId": localID()}})
			i += len(m[0])
			continue
		}
		if tag := mdTagOpen.FindStringSubmatch(s[i:]); tag != nil {
			name := tag[1] + tag[2]
			if end := closingTag(s[i+len(tag[0]):], name); end >= 0 {
				emit()
				inner := s[i+len(tag[0]) : i+len(tag[0])+end]
				out = append(out, parseInline(inner, append(slices.Clone(marks), tagMark(tag)))...)
				i += len(tag[0]) + end + len("</"+name+">")
				continue
			}
		}
		if short, n, ok := emojiAt(s, i); ok { // an emoji takes no marks
			emit()
			out = append(out, map[string]any{"type": "emoji", "attrs": map[string]any{"shortName": short, "text": emoji.Glyph(short[1 : len(short)-1])}})
			i += n
			continue
		}
		if strings.HasPrefix(s[i:], "***") { // bold and italic at once
			if end := strings.Index(s[i+3:], "***"); end > 0 {
				emit()
				both := append(slices.Clone(marks), map[string]any{"type": "strong"}, map[string]any{"type": "em"})
				out = append(out, parseInline(s[i+3:i+3+end], both)...)
				i += 3 + end + 3
				continue
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

// tableAttrs are a table's or cell's layout attributes markdown can't
// write, those away from Jira's defaults: a table with them is kept whole
// rather than saved without them.
func tableAttrs(n adfNode, names ...string) string {
	var b strings.Builder
	for _, k := range names {
		switch v := n.Attrs[k].(type) {
		case nil:
		case bool:
			if v {
				fmt.Fprintf(&b, " %s=true", k)
			}
		case string:
			if v != "" && v != "default" {
				fmt.Fprintf(&b, " %s=%s", k, v)
			}
		case float64:
			if v != 1 || k == "width" {
				fmt.Fprintf(&b, " %s=%v", k, v)
			}
		default: // colwidth: a list of widths
			fmt.Fprintf(&b, " %s=%v", k, v)
		}
	}
	return b.String()
}
