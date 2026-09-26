package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAvatarAuth: the instance's own avatars get the credentials, another
// host's never do.
func TestAvatarAuth(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte("img"))
	}))
	defer srv.Close()
	own := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if b, err := own.Avatar(context.Background(), srv.URL+"/secure/useravatar?size=large"); err != nil || string(b) != "img" || got == "" {
		t.Fatalf("own: %q %v auth %q", b, err, got)
	}
	other := New(Config{BaseURL: "https://x.atlassian.net", Email: "me@x.test", APIToken: "tok"})
	if _, err := other.Avatar(context.Background(), srv.URL+"/avatar.png"); err != nil || got != "" {
		t.Errorf("third party got auth %q (%v)", got, err)
	}
	if _, err := other.Avatar(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("a file url was fetched")
	}
}

func TestToCardAvatar(t *testing.T) {
	c := toCard("ABC-1", map[string]json.RawMessage{"assignee": json.RawMessage(`{"accountId":"a1","displayName":"Ada","avatarUrls":{"48x48":"https://a/48.png","24x24":"https://a/24.png"}}`)}, "")
	if c.AvatarURL != "https://a/48.png" || c.Assignee != "Ada" {
		t.Errorf("card = %+v", c)
	}
}
