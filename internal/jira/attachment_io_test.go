package jira

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadAttachment(t *testing.T) {
	var name, content, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("X-Atlassian-Token")
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(f)
		name, content = h.Filename, string(b)
		io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "log.txt")
	os.WriteFile(path, []byte("hello"), 0o600)
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if err := c.UploadAttachment(context.Background(), "A-1", path); err != nil {
		t.Fatal(err)
	}
	if name != "log.txt" || content != "hello" || token != "no-check" {
		t.Errorf("name %q content %q token %q", name, content, token)
	}
}

// TestDownloadAttachment: saved under its name, a taken name numbered.
func TestDownloadAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "PNG")
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	dir := t.TempDir()
	for _, want := range []string{"shot.png", "shot (1).png"} {
		path, err := c.DownloadAttachment(context.Background(), "10", "../shot.png", dir)
		if err != nil || path != filepath.Join(dir, want) {
			t.Fatalf("path %q, %v", path, err)
		}
		if b, _ := os.ReadFile(path); string(b) != "PNG" {
			t.Errorf("content %q", b)
		}
	}
}

// TestAttachmentSlowNotStalled: a download that takes longer than the
// timeout still lands while bytes keep moving; one that stops fails.
func TestAttachmentSlowNotStalled(t *testing.T) {
	stall := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := range 5 {
			if stall && i == 2 {
				time.Sleep(300 * time.Millisecond)
			}
			io.WriteString(w, "chunk")
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", Timeout: 100 * time.Millisecond})
	path, err := c.DownloadAttachment(context.Background(), "1", "a.txt", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); len(b) != 25 {
		t.Errorf("saved %d bytes", len(b))
	}
	if b, err := c.AttachmentContent(context.Background(), "1"); err != nil || len(b) != 25 {
		t.Errorf("content: %d bytes, %v", len(b), err)
	}
	stall = true
	if _, err := c.DownloadAttachment(context.Background(), "1", "b.txt", t.TempDir()); !errors.Is(err, errStalled) {
		t.Errorf("stalled download: %v", err)
	}
}
