package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIssueResolvesMedia(t *testing.T) {
	body := `{"key":"ABC-1","fields":{"summary":"s",
		"attachment":[{"id":"10","filename":"shot.png","mimeType":"image/png","size":5}],
		"description":{"type":"doc","content":[
			{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"u1","type":"file","alt":"shot.png"}}]},
			{"type":"mediaGroup","content":[{"type":"media","attrs":{"id":"u2","type":"file","alt":"gone.pdf"}}]},
			{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"u3","type":"file"}}]}
		]},
		"comment":{"total":1,"comments":[{"body":{"type":"doc","content":[
			{"type":"mediaSingle","content":[{"type":"media","attrs":{"alt":"shot.png"}}]}]}}]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	iss, err := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t"}).Get(context.Background(), "ABC-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "![shot.png](attachment:10)\n\n![gone.pdf](attachment)\n\n_[attachment]_"
	if iss.Description != want {
		t.Errorf("description = %q, want %q", iss.Description, want)
	}
	if iss.Comments[0].Body != "![shot.png](attachment:10)" {
		t.Errorf("comment = %q", iss.Comments[0].Body)
	}
	if len(iss.Attachments) != 1 || !iss.Attachments[0].IsImage() {
		t.Errorf("attachments = %+v", iss.Attachments)
	}
}

func TestAttachmentContent(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte("PNGDATA"))
	}))
	defer srv.Close()
	b, err := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t"}).AttachmentContent(context.Background(), "10")
	if err != nil || string(b) != "PNGDATA" {
		t.Fatalf("content = %q, %v", b, err)
	}
	if gotPath != "/rest/api/3/attachment/content/10" || !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("path=%q auth=%q", gotPath, gotAuth)
	}
}
