package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageID(t *testing.T) {
	c := New(Config{BaseURL: "https://acme.atlassian.net", Email: "me@x.test", APIToken: "tok"})
	for u, want := range map[string]string{
		"https://acme.atlassian.net/wiki/spaces/ENG/pages/123456/Release+plan":     "123456",
		"https://acme.atlassian.net/wiki/pages/viewpage.action?pageId=987":         "987",
		"https://other.atlassian.net/wiki/spaces/ENG/pages/1/X":                    "",
		"https://acme.atlassian.net/browse/ABC-1":                                  "",
		"https://acme.atlassian.net/wiki/pages/viewpage.action?pageId=1%3B%20drop": "",
	} {
		if got := c.PageID(u); got != want {
			t.Errorf("PageID(%s) = %q, want %q", u, got, want)
		}
	}
}

// TestConfluencePage: the page's ADF reads as markdown, a macro as a line
// naming it, an image of its own attachments by id, another by its name;
// PageImage downloads one.
func TestConfluencePage(t *testing.T) {
	media := func(id, alt string) any {
		return map[string]any{"type": "mediaSingle", "content": []any{map[string]any{"type": "media", "attrs": map[string]any{"id": id, "alt": alt}}}}
	}
	doc, _ := json.Marshal(map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": 1}, "content": []any{map[string]any{"type": "text", "text": "Plan"}}},
		map[string]any{"type": "extension", "attrs": map[string]any{"extensionKey": "toc"}},
		media("f1", "diagram.png"), media("f9", "elsewhere.png"),
	}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wiki/api/v2/pages/42":
			if r.URL.Query().Get("body-format") != "atlas_doc_format" {
				t.Errorf("%s", r.URL)
			}
			b, _ := json.Marshal(string(doc))
			fmt.Fprintf(w, `{"id":"42","title":"Release plan","body":{"atlas_doc_format":{"value":%s}},"_links":{"webui":"/spaces/ENG/pages/42"}}`, b)
		case "/wiki/api/v2/pages/42/attachments":
			fmt.Fprint(w, `{"results":[{"id":"att77","title":"diagram.png","fileId":"f1","mediaType":"image/png"}]}`)
		case "/wiki/api/v2/attachments/att77":
			fmt.Fprint(w, `{"id":"att77","downloadLink":"/download/attachments/42/diagram.png?api=v2"}`)
		case "/wiki/download/attachments/42/diagram.png":
			fmt.Fprint(w, "PNG")
		default:
			t.Errorf("%s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	p, err := c.ConfluencePage(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Release plan" || p.URL != srv.URL+"/wiki/spaces/ENG/pages/42" {
		t.Errorf("page %+v", p)
	}
	for _, want := range []string{"# Plan", "_[toc macro]_", "![diagram.png](confluence:77)", "_[image: elsewhere.png]_"} {
		if !strings.Contains(p.Markdown, want) {
			t.Errorf("no %q in:\n%s", want, p.Markdown)
		}
	}
	if b, err := c.PageImage(context.Background(), "77"); string(b) != "PNG" || err != nil {
		t.Errorf("PageImage = %q, %v", b, err)
	}
}
