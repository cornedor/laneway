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
// line was moved; a deleted line drops its block.
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
		"<!-- keep:2 paragraph with a mention: move or delete this line -->\n\n" +
		"<!-- keep:3 paragraph: move or delete this line -->"
	if ed.Markdown != want || len(ed.Kept) != 3 {
		t.Fatalf("markdown:\n%s\nkept %d", ed.Markdown, len(ed.Kept))
	}
	edited := "<!-- keep:2 moved up -->\n\nnew intro\n\n<!-- keep:1 table -->"
	out, _ := json.Marshal(MarkdownToADFKept(edited, ed.Kept))
	var got struct {
		Content []json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(out, &got)
	if len(got.Content) != 3 || string(got.Content[0]) != mention || string(got.Content[2]) != table {
		t.Errorf("saved = %s", out)
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
