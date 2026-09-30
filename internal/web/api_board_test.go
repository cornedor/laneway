package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

func TestAvatarProxy(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	hits := 0
	jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/text" {
			_, _ = w.Write([]byte("hello"))
			return
		}
		_, _ = w.Write(png)
	}))
	defer jiraSrv.Close()
	client := jira.New(jira.Config{BaseURL: jiraSrv.URL, Email: "a@b.c", APIToken: "x"})
	srv := New(context.Background(), Options{Client: client, AllowedHosts: []string{"example.com"}})

	get := func(u string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", "/api/avatar?u="+url.QueryEscape(u), nil))
		return rec
	}
	for i := 0; i < 2; i++ {
		rec := get(jiraSrv.URL + "/a.png")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("avatar = %d %q", rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if hits != 1 {
		t.Errorf("upstream hits = %d, want 1 (cached)", hits)
	}
	if rec := get("http://169.254.169.254/latest"); rec.Code != 400 {
		t.Errorf("foreign host = %d, want 400", rec.Code)
	}
	if rec := get(jiraSrv.URL + "/text"); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-image = %d, want 415", rec.Code)
	}
	if rec := get(""); rec.Code != 400 {
		t.Errorf("empty = %d, want 400", rec.Code)
	}
}

func TestAvatarType(t *testing.T) {
	for body, want := range map[string]string{
		"<svg xmlns='x'/>":               "image/svg+xml",
		"<?xml version='1'?><svg/>":      "image/svg+xml",
		"<html><script>":                 "",
		"GIF89a\x01\x00\x01\x00\x00\x00": "image/gif",
	} {
		if got := avatarType([]byte(body)); got != want {
			t.Errorf("avatarType(%q) = %q, want %q", body, got, want)
		}
	}
}
