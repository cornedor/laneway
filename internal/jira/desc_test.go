package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// editableDocs round trip: markdown out, the same document back.
var editableDocs = []string{
	`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Go","marks":[{"type":"link","attrs":{"href":"https://en.wikipedia.org/wiki/Go_(lang)"}}]}]}]}`,
	`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello "},{"type":"text","text":"bold","marks":[{"type":"strong"}]},{"type":"text","text":" and "},{"type":"text","text":"it","marks":[{"type":"em"}]}]}]}`,
	`{"type":"doc","content":[{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Steps"}]},
	  {"type":"orderedList","attrs":{"order":1},"content":[
	    {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
	      {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested "},{"type":"text","text":"code","marks":[{"type":"code"}]}]}]}]}]},
	    {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]}]}`,
	`{"type":"doc","content":[{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1\ny := 2"}]},
	  {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},{"type":"rule"},
	  {"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://x.test/a"}}]},{"type":"text","text":" or "},{"type":"text","text":"old","marks":[{"type":"strike"}]}]},
	  {"type":"paragraph","content":[{"type":"text","text":"line one"},{"type":"hardBreak"},{"type":"text","text":"line two with snake_case"}]}]}`,
}

func TestEditableDescription(t *testing.T) {
	for i, doc := range editableDocs {
		ed, err := EditableDescription(json.RawMessage(doc))
		if err != nil || len(ed.Kept) != 0 {
			t.Errorf("doc %d: %v, kept %d", i, err, len(ed.Kept))
			continue
		}
		md := ed.Markdown
		back, _ := json.Marshal(MarkdownToADF(md))
		if got := adfToMarkdown(back); got != md {
			t.Errorf("doc %d markdown changed:\n%s\n---\n%s", i, md, got)
		}
	}
	if ed, err := EditableDescription(nil); err != nil || ed.Markdown != "" {
		t.Errorf("empty = %q, %v", ed.Markdown, err)
	}
}

// TestEditableDescriptionKeeps: blocks markdown can't keep become
// placeholder lines, and saving puts them back untouched, wherever the
// line was moved; a deleted line drops its block. A mention stays inline
// as ⟦N @name⟧, its paragraph editable around it; stars that are text are
// escaped.
func TestEditableDescriptionKeeps(t *testing.T) {
	table := `{"type":"table","attrs":{"layout":"default"},"content":[{"type":"tableRow","content":[]}]}`
	mention := `{"type":"paragraph","content":[{"type":"text","text":"ping "},{"type":"mention","attrs":{"id":"x","text":"@Ann"}}]}`
	stars := `{"type":"paragraph","content":[{"type":"text","text":"5 * 3 * 2"}]}`
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"intro"}]},` + table + `,` + mention + `,` + stars + `]}`
	ed, err := EditableDescription(json.RawMessage(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := "intro\n\n<!-- keep:1 table: move or delete this line -->\n\n" +
		"ping @Ann\n\n" +
		`5 \* 3 \* 2`
	if ed.Markdown != want || len(ed.Kept) != 2 {
		t.Fatalf("markdown:\n%s\nkept %d", ed.Markdown, len(ed.Kept))
	}
	edited := "hey ⟦2 renamed⟧, ⟦1 not inline⟧ **ok**\n\nnew intro\n\n<!-- keep:1 table -->"
	out, _ := json.Marshal(MarkdownToADFKept(edited, ed.Kept))
	var got struct {
		Content []json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(out, &got)
	para := `{"content":[{"text":"hey ","type":"text"},{"attrs":{"id":"x","text":"@Ann"},"type":"mention"},` +
		`{"text":", ⟦1 not inline⟧ ","type":"text"},{"marks":[{"type":"strong"}],"text":"ok","type":"text"}],"type":"paragraph"}`
	if len(got.Content) != 3 || string(got.Content[0]) != para || string(got.Content[2]) != table {
		t.Errorf("saved = %s", out)
	}
}

// TestEditableEscapes: text that reads as markdown ("**not bold**", "# 1",
// a backslash) edits escaped and saves as it was, not as a placeholder
// that would bring the old text back.
func TestEditableEscapes(t *testing.T) {
	for _, text := range []string{"before edit **bold**", "# not a heading", "- not a list", "1. no", "a \\ b `c` [d] ~~e~~", "<!-- keep:1 x -->"} {
		raw, _ := json.Marshal(map[string]any{"type": "doc", "version": 1, "content": []any{
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}})
		ed, err := EditableDescription(raw)
		if err != nil || len(ed.Kept) != 0 {
			t.Errorf("%q: %q, kept %d, %v", text, ed.Markdown, len(ed.Kept), err)
			continue
		}
		back, _ := json.Marshal(MarkdownToADFKept(ed.Markdown, ed.Kept))
		if got := adfToMarkdown(back); got != text {
			t.Errorf("%q came back %q (edited as %q)", text, got, ed.Markdown)
		}
	}
	// Bold stays bold, around escaped text.
	doc, _ := json.Marshal(MarkdownToADF(`**5 \* 3** \[x]`))
	if got := string(doc); !strings.Contains(got, `"text":"5 * 3"`) || !strings.Contains(got, `"text":" [x]"`) {
		t.Errorf("escapes = %s", got)
	}
}

// TestMarkdownToADF: markdown written by hand, * bullets and all.
func TestMarkdownToADF(t *testing.T) {
	doc, _ := json.Marshal(MarkdownToADF("# Title\n\n* a\n* b\n  1. c\n\nplain **bold [link](https://x.test)**"))
	got := adfToMarkdown(doc)
	want := "# Title\n\n- a\n- b\n  1. c\n\nplain **bold **[**link**](https://x.test)"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSetDescription(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if err := c.SetDescription(context.Background(), "ABC-1", " ", nil); err != nil {
		t.Fatal(err)
	}
	if body != `{"fields":{"description":null}}` {
		t.Errorf("blank body = %s", body)
	}
	_ = c.SetDescription(context.Background(), "ABC-1", "hi", nil)
	if !strings.Contains(body, `"type":"doc"`) || !strings.Contains(body, `"text":"hi"`) {
		t.Errorf("body = %s", body)
	}
}

func TestInlineMentions(t *testing.T) {
	doc := textToADF("thanks @Ada Lovelace and @Bob, see @Ada", nil)
	inlineMentions(doc, []Mention{{AccountID: "a1", DisplayName: "Ada Lovelace"}, {AccountID: "b1", DisplayName: "Bob"}})
	b, _ := json.Marshal(doc)
	got := string(b)
	for _, want := range []string{`"id":"a1","text":"@Ada Lovelace"`, `"id":"b1","text":"@Bob"`, `"text":", see @Ada"`} {
		if !strings.Contains(got, want) {
			t.Errorf("doc lacks %s: %s", want, got)
		}
	}
}

func TestInlineLabel(t *testing.T) {
	for _, c := range []struct {
		n    adfNode
		want string
	}{
		{adfNode{Type: "date", Attrs: map[string]any{"timestamp": "1767225600000"}}, "2026-01-01"},
		{adfNode{Type: "emoji", Attrs: map[string]any{"shortName": ":smile:"}}, ":smile:"},
		{adfNode{Type: "inlineCard", Attrs: map[string]any{"url": "https://x.test/a_(b)"}}, "https://x.test/a_b"},
		{adfNode{Type: "status"}, "status"},
	} {
		if got := inlineLabel(c.n); got != c.want {
			t.Errorf("%s: %q, want %q", c.n.Type, got, c.want)
		}
	}
}

// TestEditablePanel: a panel edits as its blocks between marker lines, and
// saves back as a panel of its type, a mention in it kept inline.
func TestEditablePanel(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"panel","attrs":{"panelType":"success","localId":"p1"},"content":[
	  {"type":"paragraph","content":[{"type":"text","text":"Done when:","marks":[{"type":"strong"}]}]},
	  {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"it works for "},{"type":"mention","attrs":{"id":"x","text":"@Ann"}}]}]}]}]},
	  {"type":"paragraph","content":[{"type":"text","text":"after"}]}]}`
	ed, err := EditableDescription(json.RawMessage(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- panel:success -->\n\n**Done when:**\n\n- it works for @Ann\n\n<!-- /panel -->\n\nafter"
	if ed.Markdown != want {
		t.Fatalf("markdown:\n%s\nwant\n%s", ed.Markdown, want)
	}
	edited := strings.Replace(ed.Markdown, "Done when:", "Ready when:", 1)
	out, _ := json.Marshal(MarkdownToADFKept(edited, ed.Kept))
	for _, s := range []string{`"type":"panel"`, `"panelType":"success"`, `"text":"Ready when:"`, `"type":"mention"`} {
		if !strings.Contains(string(out), s) {
			t.Errorf("saved lacks %s: %s", s, out)
		}
	}
	var got struct {
		Content []struct{ Type string } `json:"content"`
	}
	_ = json.Unmarshal(out, &got)
	if len(got.Content) != 2 || got.Content[0].Type != "panel" || got.Content[1].Type != "paragraph" {
		t.Errorf("saved blocks = %+v", got.Content)
	}
}

// TestEditableTrailingSpace: a space ending a paragraph, gone in markdown
// and unseen in Jira, doesn't make the description uneditable.
func TestEditableTrailingSpace(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"due on "},
	  {"type":"date","attrs":{"timestamp":"1788220800000"}},{"type":"text","text":" or later. "}]}]}`
	ed, err := EditableDescription(json.RawMessage(doc))
	if err != nil || len(ed.Kept) != 0 || ed.Markdown != "due on <date>2026-09-01</date> or later." {
		t.Fatalf("%q, kept %d, %v", ed.Markdown, len(ed.Kept), err)
	}
}

// TestEmoji: a known :shortcode: saves as Jira's emoji node, not as text;
// an emoji node edits as its shortcode and saves back as one; text that
// only looks like one (a time, an unknown name) stays text.
func TestEmoji(t *testing.T) {
	doc, _ := json.Marshal(MarkdownToADF("ship it :rocket: at 10:30 :nosuchemoji: **:tada:**"))
	got := string(doc)
	for _, want := range []string{`{"attrs":{"shortName":":rocket:","text":"🚀"},"type":"emoji"}`, `"shortName":":tada:"`, `at 10:30 :nosuchemoji: `} {
		if !strings.Contains(got, want) {
			t.Errorf("saved lacks %s:\n%s", want, got)
		}
	}
	para := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"nice "},{"type":"emoji","attrs":{"shortName":":thumbsup:","id":"1f44d","text":"👍"}},{"type":"text","text":" but :smile: is typed"}]}]}`
	ed, err := EditableDescription(json.RawMessage(para))
	if err != nil || len(ed.Kept) != 1 || ed.Markdown != `nice :thumbsup: but \:smile: is typed` {
		t.Fatalf("%q, kept %d, %v", ed.Markdown, len(ed.Kept), err)
	}
	back, _ := json.Marshal(MarkdownToADFKept(ed.Markdown, ed.Kept))
	if s := string(back); !strings.Contains(s, `"shortName":":thumbsup:"`) || !strings.Contains(s, `" but :smile: is typed"`) {
		t.Errorf("saved = %s", s)
	}
}

// TestEmojiShows: an emoji Jira names in a way the table doesn't know (a
// skin tone, a custom one) shows as its text.
func TestEmojiShows(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[
	  {"type":"emoji","attrs":{"shortName":":thumbsup::skin-tone-2:","id":"1f44d","text":"👍🏽"}},
	  {"type":"emoji","attrs":{"shortName":":awthanks:","id":"atlassian-awthanks","text":":awthanks:"}},
	  {"type":"emoji","attrs":{"shortName":":grinning:"}}]}]}`
	if got := adfToMarkdown(json.RawMessage(doc)); got != "👍🏽:awthanks::grinning:" {
		t.Errorf("shows %q", got)
	}
}

// TestTextEmoji: a plain-text comment's :shortcode: posts as an emoji too.
func TestTextEmoji(t *testing.T) {
	doc, _ := json.Marshal(textToADF("done :tada: at 10:30:00 `:x:`", nil))
	got := string(doc)
	if !strings.Contains(got, `{"text":"done ","type":"text"},{"attrs":{"shortName":":tada:","text":"🎉"},"type":"emoji"},{"text":" at 10:30:00 `+"`:x:`"+`","type":"text"}`) {
		t.Errorf("doc = %s", got)
	}
}

// TestEditableRich: tasks, decisions, tables, expands, a list starting
// past 1 and the marks markdown lacks edit as text and come back the same.
func TestEditableRich(t *testing.T) {
	docs := map[string]struct{ adf, md string }{
		"tasks": {`{"type":"doc","content":[{"type":"taskList","attrs":{"localId":"a"},"content":[
		  {"type":"taskItem","attrs":{"localId":"b","state":"TODO"},"content":[{"type":"text","text":"write it"}]},
		  {"type":"taskList","attrs":{"localId":"c"},"content":[{"type":"taskItem","attrs":{"localId":"d","state":"DONE"},"content":[{"type":"text","text":"test it"}]}]},
		  {"type":"taskItem","attrs":{"localId":"e","state":"TODO"},"content":[]}]}]}`,
			"- [ ] write it\n  - [x] test it\n- [ ]"},
		"decision": {`{"type":"doc","content":[{"type":"decisionList","attrs":{"localId":"a"},"content":[
		  {"type":"decisionItem","attrs":{"localId":"b","state":"DECIDED"},"content":[{"type":"text","text":"ship "},{"type":"text","text":"Friday","marks":[{"type":"strong"}]}]}]}]}`,
			"<> ship **Friday**"},
		"table": {`{"type":"doc","content":[{"type":"table","attrs":{"layout":"default"},"content":[
		  {"type":"tableRow","content":[{"type":"tableHeader","attrs":{},"content":[{"type":"paragraph","content":[{"type":"text","text":"a|b","marks":[{"type":"strong"}]}]}]},{"type":"tableHeader","attrs":{},"content":[{"type":"paragraph","content":[]}]}]},
		  {"type":"tableRow","content":[{"type":"tableCell","attrs":{},"content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]},{"type":"paragraph","content":[]}]},{"type":"tableCell","attrs":{},"content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"code"}]}]}]}]}]}]}`,
			"| **a\\|b** |  |\n| --- | --- |\n| 1 | `x` |"},
		"headless table": {`{"type":"doc","content":[{"type":"table","content":[
		  {"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"2"}]}]}]}]}]}`,
			"|  |  |\n| --- | --- |\n| 1 | 2 |"},
		"expand": {`{"type":"doc","content":[{"type":"expand","attrs":{"title":"Spoilers!"},"content":[{"type":"paragraph","content":[{"type":"text","text":"hidden"}]}]}]}`,
			"<!-- expand: Spoilers! -->\n\nhidden\n\n<!-- /expand -->"},
		"order": {`{"type":"doc","content":[{"type":"orderedList","attrs":{"order":3},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"third"}]}]}]}]}`,
			"3. third"},
		"marks": {`{"type":"doc","content":[{"type":"paragraph","content":[
		  {"type":"text","text":"u","marks":[{"type":"underline"}]},{"type":"text","text":" H"},{"type":"text","text":"2","marks":[{"type":"subsup","attrs":{"type":"sub"}}]},
		  {"type":"text","text":"O "},{"type":"text","text":"hot","marks":[{"type":"strong"},{"type":"textColor","attrs":{"color":"#ff5630"}}]},
		  {"type":"text","text":" ","marks":[{"type":"textColor","attrs":{"color":"#ff5630"}}]},
		  {"type":"text","text":"lit","marks":[{"type":"backgroundColor","attrs":{"color":"#fff0b3"}}]},{"type":"text","text":" <u>raw</u>"}]}]}`,
			`<u>u</u> H<sub>2</sub>O <span style="color:#ff5630">**hot**</span> <span style="background-color:#fff0b3">lit</span> \<u>raw\</u>`},
		"gaps": {`{"type":"doc","content":[{"type":"paragraph","content":[]},{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"}]},{"type":"paragraph","content":[{"type":"text","text":" "}]},{"type":"paragraph","content":[{"type":"text","text":"| not a table"}]}]}`,
			`a` + "\n\n" + `\| not a table`},
	}
	for name, d := range docs {
		ed, err := EditableDescription(json.RawMessage(d.adf))
		if err != nil || len(ed.Kept) != 0 || ed.Markdown != d.md {
			t.Errorf("%s: %v, kept %d:\n%s\nwant\n%s", name, err, len(ed.Kept), ed.Markdown, d.md)
		}
	}
}

// TestEditableContainer: a block with a node markdown can't carry (a
// synced block) edits as its content between block markers, its node put
// back around the edited content on save.
func TestEditableContainer(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"bodiedSyncBlock","attrs":{"resourceId":"r1"},"content":[
	  {"type":"paragraph","content":[{"type":"text","text":"shared"}]},
	  {"type":"panel","attrs":{"panelType":"custom","panelColor":"#abcdef"},"content":[{"type":"paragraph","content":[{"type":"text","text":"tinted"}]}]}]}]}`
	ed, err := EditableDescription(json.RawMessage(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- block:1 synced block -->\n\nshared\n\n<!-- block:2 custom panel -->\n\ntinted\n\n<!-- /block -->\n\n<!-- /block -->"
	if ed.Markdown != want {
		t.Fatalf("markdown:\n%s", ed.Markdown)
	}
	out, _ := json.Marshal(MarkdownToADFKept(strings.Replace(ed.Markdown, "tinted", "tinted **more**", 1), ed.Kept))
	for _, s := range []string{`"resourceId":"r1"`, `"panelColor":"#abcdef"`, `"text":"more"`} {
		if !strings.Contains(string(out), s) {
			t.Errorf("saved lacks %s: %s", s, out)
		}
	}
}

// TestMarkdownToADFRich: tasks and decisions get the ids Jira requires,
// empty ones an empty content rather than null; a table's cells are
// paragraphs.
func TestMarkdownToADFRich(t *testing.T) {
	out, _ := json.Marshal(MarkdownToADF("- [ ]\n- [X] done\n\n<> yes\n\n| h |\n|---|\n| c |"))
	got := string(out)
	for _, s := range []string{`"state":"TODO"`, `"state":"DONE"`, `"state":"DECIDED"`, `"localId":"`,
		`"type":"tableHeader"`, `"type":"tableCell"`, `"content":[]`} {
		if !strings.Contains(got, s) {
			t.Errorf("lacks %s: %s", s, got)
		}
	}
	if strings.Contains(got, "null") {
		t.Errorf("a null: %s", got)
	}
}

// TestToggleTask: the nth item outside tables flips, the rest of the
// document untouched; a count or state other than shown writes nothing.
func TestToggleTask(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","version":1,"content":[
	  {"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"taskList","attrs":{"localId":"t"},"content":[{"type":"taskItem","attrs":{"localId":"x","state":"TODO"},"content":[]}]}]}]}]},
	  {"type":"taskList","attrs":{"localId":"a"},"content":[
	    {"type":"taskItem","attrs":{"localId":"b","state":"TODO"},"content":[{"type":"text","text":"one"}]},
	    {"type":"taskList","attrs":{"localId":"c"},"content":[{"type":"taskItem","attrs":{"localId":"d","state":"DONE"},"content":[{"type":"text","text":"two"}]}]}]}]}`)
	doc, err := toggleTask(raw, 2, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(doc)
	if strings.Count(string(out), `"state":"TODO"`) != 3 || !strings.Contains(string(out), `"localId":"d","state":"TODO"`) {
		t.Errorf("toggled = %s", out)
	}
	for _, c := range [][3]int{{2, 3, 0}, {1, 2, 0}, {3, 2, 1}} { // wrong total; already TODO; out of range
		if _, err := toggleTask(raw, c[0], c[1], c[2] == 1); err == nil {
			t.Errorf("%v: no error", c)
		}
	}
}

// TestEditableInline: mentions, statuses, dates, smart links, cards,
// custom emoji and a coloured table with a header column edit as text
// and save back as the nodes they were; "@Ann" typed as words stays text.
func TestEditableInline(t *testing.T) {
	doc := `{"type":"doc","content":[
	  {"type":"paragraph","content":[{"type":"mention","attrs":{"id":"a1","text":"@Ann Lee","accessLevel":""}},{"type":"text","text":" is "},
	    {"type":"status","attrs":{"text":"Blocked","color":"red","localId":"s"}},{"type":"text","text":" till "},{"type":"date","attrs":{"timestamp":"1790121600000"}},
	    {"type":"text","text":", see "},{"type":"inlineCard","attrs":{"url":"https://x.test/a"}},{"type":"text","text":" "},
	    {"type":"emoji","attrs":{"shortName":":1_one_square_blue:","id":"atlassian-1_one_square_blue","text":":1_one_square_blue:"}},{"type":"text","text":" not @Ann Lee"}]},
	  {"type":"blockCard","attrs":{"url":"https://x.test/b"}},
	  {"type":"table","content":[
	    {"type":"tableRow","content":[{"type":"tableHeader","attrs":{"background":"#deebff"},"content":[{"type":"paragraph","content":[{"type":"text","text":"h"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"i"}]}]}]},
	    {"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"row"}]}]},{"type":"tableCell","attrs":{"background":"#e3fcef"},"content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]}]}]}]}`
	ed, err := EditableDescription(json.RawMessage(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := `@Ann Lee is <status color="red">BLOCKED</status> till <date>2026-09-23</date>, see <https://x.test/a> :1_one_square_blue: not \@Ann Lee` + "\n\n" +
		"<!-- card: https://x.test/b -->\n\n" +
		"| <!-- bg:#deebff --> h | i |\n| --- | --- |\n| <!-- th --> row | <!-- bg:#e3fcef --> 1 |"
	if ed.Markdown != want || strings.Contains(ed.Markdown, "keep:") {
		t.Fatalf("markdown:\n%s", ed.Markdown)
	}
	out, _ := json.Marshal(MarkdownToADFKept(strings.Replace(ed.Markdown, "BLOCKED", "Unblocked", 1), ed.Kept))
	s := string(out)
	for _, w := range []string{`"id":"a1"`, `"text":"Unblocked"`, `"timestamp":"1790121600000"`, `"type":"inlineCard"`, `"id":"atlassian-1_one_square_blue"`,
		`"text":" not @Ann Lee"`, `"type":"blockCard"`, `"background":"#e3fcef"`} {
		if !strings.Contains(s, w) {
			t.Errorf("saved lacks %s: %s", w, s)
		}
	}
	if strings.Count(s, `"type":"mention"`) != 1 || strings.Count(s, `"type":"tableHeader"`) != 3 || strings.Contains(s, "") {
		t.Errorf("saved = %s", s)
	}
}

// TestBoldItalic: "***x***" is bold and italic, not bold "*x" and a star.
func TestBoldItalic(t *testing.T) {
	out, _ := json.Marshal(MarkdownToADF("***both***"))
	if s := string(out); !strings.Contains(s, `"marks":[{"type":"strong"},{"type":"em"}],"text":"both"`) {
		t.Errorf("%s", s)
	}
}

// TestEditableKeepsWhatMarkdownLoses: a sized or wide table and a link with
// a title stand kept rather than saved without those; a default table
// still edits. A mention elsewhere doesn't make an inline placeholder take
// its paragraph.
func TestEditableKeepsWhatMarkdownLoses(t *testing.T) {
	cell := func(attrs string) string {
		return `{"type":"tableCell","attrs":{` + attrs + `},"content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]}`
	}
	table := func(attrs, cellAttrs string) string {
		return `{"type":"doc","content":[{"type":"table","attrs":{` + attrs + `},"content":[{"type":"tableRow","content":[` + cell(cellAttrs) + `]}]}]}`
	}
	for _, tc := range []struct {
		name, doc string
		kept      bool
	}{
		{"default table", table(`"isNumberColumnEnabled":false,"layout":"default","localId":"x"`, `"colspan":1,"rowspan":1`), false},
		{"wide table", table(`"layout":"wide"`, ``), true},
		{"sized column", table(`"layout":"default"`, `"colwidth":[240]`), true},
		{"numbered", table(`"isNumberColumnEnabled":true`, ``), true},
		{"link title", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Go","marks":[{"type":"link","attrs":{"href":"https://go.dev","title":"The Go site"}}]}]}]}`, true},
	} {
		ed, err := EditableDescription(json.RawMessage(tc.doc))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if kept := strings.Contains(ed.Markdown, "<!-- keep:"); kept != tc.kept {
			t.Errorf("%s: kept %v, want %v:\n%s", tc.name, kept, tc.kept, ed.Markdown)
		}
	}
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"a1","text":"@Ann"}}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"mediaInline","attrs":{"id":"m1","type":"file"}}]}]}`
	ed, err := EditableDescription(json.RawMessage(doc))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ed.Markdown, "<!-- keep:") || !strings.Contains(ed.Markdown, "see ⟦1") {
		t.Errorf("the mediaInline paragraph should edit with ⟦1⟧:\n%s", ed.Markdown)
	}
}

// TestADFShowsWhatItCannotEdit: an inline file and macro are named, an
// image's caption shows under it, rather than vanishing from the panel.
func TestADFShowsWhatItCannotEdit(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"mediaInline","attrs":{"id":"m1"}},{"type":"text","text":" and "},{"type":"inlineExtension","attrs":{"extensionKey":"jira-chart"}}]},` +
		`{"type":"mediaSingle","content":[{"type":"media","attrs":{"alt":"shot.png"}},{"type":"caption","content":[{"type":"text","text":"The new flow"}]}]}]}`
	md := adfToMarkdown(json.RawMessage(doc))
	for _, want := range []string{"see _[file]_ and _[jira-chart macro]_", "![shot.png](attachment)", "_The new flow_"} {
		if !strings.Contains(md, want) {
			t.Errorf("no %q in:\n%s", want, md)
		}
	}
	if _, err := EditableDescription(json.RawMessage(doc)); err != nil {
		t.Errorf("still editable around them: %v", err)
	}
}
