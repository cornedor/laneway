package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestDownloadAttachmentPrivate: a download and its new folder are the
// user's alone.
func TestDownloadAttachmentPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PDF"))
	}))
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "downloads")
	path, err := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t"}).DownloadAttachment(context.Background(), "10", "contract.pdf", dir)
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v", p, fi.Mode().Perm(), want)
		}
	}
}

// TestCommentParentID: the undocumented parentId comes through as a string
// or a number, and is "" when absent or of another shape.
// TestCommentParentID: Jira's issue lists comments without parentId; the
// comment endpoint has it (string, number, or neither), so that is read.
func TestCommentParentID(t *testing.T) {
	issue := `{"key":"ABC-1","fields":{"summary":"s","comment":{"total":4,"comments":[
		{"id":"1","body":null},{"id":"2","body":null},{"id":"3","body":null},{"id":"4","body":null}]}}}`
	endpoint := `{"total":4,"comments":[
		{"id":"1","body":null},
		{"id":"2","parentId":"1","body":null},
		{"id":"3","parentId":1,"body":null},
		{"id":"4","parentId":{"id":"1"},"body":null}]}`
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/ABC-1/comment" && fail:
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/rest/api/3/issue/ABC-1/comment":
			_, _ = w.Write([]byte(endpoint))
		default:
			_, _ = w.Write([]byte(issue))
		}
	}))
	defer srv.Close()
	flat := false
	parents := func() string {
		c := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t", FlatReplies: flat})
		iss, err := c.Get(context.Background(), "ABC-1")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, c := range iss.Comments {
			got = append(got, c.ID+":"+c.ParentID)
		}
		return strings.Join(got, ",")
	}
	if got := parents(); got != "1:,2:1,3:1,4:" {
		t.Errorf("parent ids = %q", got)
	}
	// The endpoint failing keeps the issue's comments, flat.
	fail = true
	if got := parents(); got != "1:,2:,3:,4:" {
		t.Errorf("without the endpoint = %q", got)
	}
	// ui.threaded_replies off: the issue's own list, no extra request.
	fail, flat = false, true
	if got := parents(); got != "1:,2:,3:,4:" {
		t.Errorf("flat = %q", got)
	}
}

// TestCommentVisibilityRead: a comment's visibility comes through: a role,
// a group, a Service Desk internal note; jsdPublic true is for everyone.
func TestCommentVisibilityRead(t *testing.T) {
	issue := `{"key":"ABC-1","fields":{"summary":"s","comment":{"total":4,"comments":[
		{"id":"1","body":null,"jsdPublic":true},
		{"id":"2","body":null,"visibility":{"type":"role","value":"Developers","identifier":"Developers"}},
		{"id":"3","body":null,"visibility":{"type":"group","value":"devs","identifier":"g1"}},
		{"id":"4","body":null,"jsdPublic":false}]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(issue))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "e", APIToken: "t", FlatReplies: true})
	iss, err := c.Get(context.Background(), "ABC-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Visibility{{}, {Role: "Developers"}, {Group: "devs", GroupID: "g1"}, {Internal: true}}
	for i, cm := range iss.Comments {
		if cm.Visibility != want[i] {
			t.Errorf("comment %s: %+v, want %+v", cm.ID, cm.Visibility, want[i])
		}
	}
}

func TestResolveMediaByID(t *testing.T) {
	atts := []Attachment{{ID: "10", Filename: "a.png"}, {ID: "11", Filename: "a.png"}, {ID: "12", Filename: "b.png"}}
	media := map[string]Attachment{"aa-11": atts[1]}
	md := "![a.png](attachment#aa-11 \"320px wrap-left\")\n\n![b.png](attachment#aa-ff)\n\n![](attachment#aa-99)\n\n![c.png](attachment)"
	want := "![a.png](attachment:11 \"320px wrap-left\")\n\n![b.png](attachment:12)\n\n_[attachment]_\n\n![c.png](attachment)"
	if got := resolveMedia(md, atts, media); got != want {
		t.Errorf("resolveMedia =\n%s\nwant\n%s", got, want)
	}
}

func TestMediaTitle(t *testing.T) {
	for _, c := range []struct {
		attrs map[string]any
		want  string
	}{
		{map[string]any{"layout": "center"}, ""},
		{map[string]any{"layout": "wrap-left", "width": 320.0, "widthType": "pixel"}, ` "320px wrap-left"`},
		{map[string]any{"layout": "center", "width": 66.5}, ` "66.5%"`},
		{map[string]any{"layout": "bad layout"}, ""},
	} {
		if got := mediaTitle(c.attrs); got != c.want {
			t.Errorf("mediaTitle(%v) = %q, want %q", c.attrs, got, c.want)
		}
	}
}
