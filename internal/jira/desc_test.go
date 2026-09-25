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
		md, err := EditableDescription(json.RawMessage(doc))
		if err != nil {
			t.Errorf("doc %d: %v", i, err)
			continue
		}
		back, _ := json.Marshal(MarkdownToADF(md))
		if got := adfToMarkdown(back); got != md {
			t.Errorf("doc %d markdown changed:\n%s\n---\n%s", i, md, got)
		}
	}
	if md, err := EditableDescription(nil); err != nil || md != "" {
		t.Errorf("empty = %q, %v", md, err)
	}
}

func TestEditableDescriptionRefuses(t *testing.T) {
	for want, doc := range map[string]string{
		"a table":        `{"type":"doc","content":[{"type":"table","content":[]}]}`,
		"a mention":      `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"x"}}]}]}`,
		"underline text": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"u","marks":[{"type":"underline"}]}]}]}`,
		"not starting":   `{"type":"doc","content":[{"type":"orderedList","attrs":{"order":3},"content":[]}]}`,
		"would change":   `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"5 * 3 * 2"}]}]}`,
	} {
		if _, err := EditableDescription(json.RawMessage(doc)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
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
	if err := c.SetDescription(context.Background(), "ABC-1", " "); err != nil {
		t.Fatal(err)
	}
	if body != `{"fields":{"description":null}}` {
		t.Errorf("blank body = %s", body)
	}
	_ = c.SetDescription(context.Background(), "ABC-1", "hi")
	if !strings.Contains(body, `"type":"doc"`) || !strings.Contains(body, `"text":"hi"`) {
		t.Errorf("body = %s", body)
	}
}
