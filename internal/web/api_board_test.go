package web

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		id := "unknown"
		if u != "" {
			id = avatarURLs.register(u)
		}
		srv.ServeHTTP(rec, httptest.NewRequest("GET", "/api/avatar/"+id, nil))
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
	if rec := get(""); rec.Code != 404 {
		t.Errorf("unknown id = %d, want 404", rec.Code)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/api/avatar?u="+url.QueryEscape(jiraSrv.URL+"/a.png"), nil))
	if rec.Code == 200 {
		t.Error("?u= still served")
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

func TestRewriteAvatars(t *testing.T) {
	in := `{"AvatarURL":"https://secure.gravatar.com/avatar/a?d=mm\u0026s=48","Cards":[{"AuthorAvatar":"http://x/a","TypeAvatar":"https://x/t","Reviewers":[{"Name":"n","Avatar":"https://x/b"}]}],"avatar":"","Name":"\"AvatarURL\":\"https://x\""}`
	out := string(rewriteAvatars([]byte(in)))
	var v struct {
		AvatarURL string
		Cards     []struct {
			AuthorAvatar string
			TypeAvatar   string
			Reviewers    []struct{ Avatar string }
		}
		Name string
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, got := range []string{v.AvatarURL, v.Cards[0].AuthorAvatar, v.Cards[0].TypeAvatar, v.Cards[0].Reviewers[0].Avatar} {
		id, ok := strings.CutPrefix(got, "/api/avatar/")
		if !ok {
			t.Fatalf("not rewritten: %s", out)
		}
		if _, ok := avatarURLs.lookup(id); !ok {
			t.Errorf("id %s not registered", id)
		}
	}
	id := strings.TrimPrefix(v.AvatarURL, "/api/avatar/")
	if u, _ := avatarURLs.lookup(id); u != "https://secure.gravatar.com/avatar/a?d=mm&s=48" {
		t.Errorf("registered %q", u)
	}
	if !strings.Contains(out, `"avatar":""`) {
		t.Errorf("empty value touched: %s", out)
	}
}

func TestAvatarRegistryBound(t *testing.T) {
	g := &avatarRegistry{max: 3, ll: list.New(), by: map[string]*list.Element{}}
	first := g.register("https://x/0")
	for i := 1; i < 5; i++ {
		g.register(fmt.Sprint("https://x/", i))
	}
	if _, ok := g.lookup(first); ok {
		t.Error("oldest not evicted")
	}
	if g.ll.Len() != 3 || len(g.by) != 3 {
		t.Errorf("size %d/%d", g.ll.Len(), len(g.by))
	}
	if g.register("https://x/4") != g.register("https://x/4") {
		t.Error("unstable id")
	}
}
