package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	got = "unset"
	if _, err := other.Avatar(context.Background(), srv.URL+"/avatar.png"); err == nil || got != "unset" {
		t.Errorf("another host was fetched (auth %q)", got)
	}
	if _, err := other.Avatar(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("a file url was fetched")
	}
}

// TestAvatarHosts: only the instance and Atlassian's avatar hosts.
func TestAvatarHosts(t *testing.T) {
	c := New(Config{BaseURL: "https://x.atlassian.net", Email: "me@x.test", APIToken: "tok"})
	for raw, want := range map[string]bool{
		"https://x.atlassian.net/secure/useravatar":                                    true,
		"https://secure.gravatar.com/avatar/abc?d=mm":                                  true,
		"https://avatar-management--avatars.us-west-2.prod.public.atl-paas.net/a/b/48": true,
		"http://secure.gravatar.com/avatar/abc":                                        false,
		"https://atl-paas.net.evil.test/a":                                             false,
		"https://evilatl-paas.net/a":                                                   false,
		"https://secure.gravatar.com:8443/avatar/abc":                                  false,
		"http://169.254.169.254/latest/meta-data":                                      false,
		"http://localhost:8080/admin":                                                  false,
	} {
		u, _ := url.Parse(raw)
		if got := c.avatarHost(u); got != want {
			t.Errorf("avatarHost(%s) = %v, want %v", raw, got, want)
		}
	}
}

// TestAvatarRedirect: a redirect off the allowed hosts is not followed.
func TestAvatarRedirect(t *testing.T) {
	var hit bool
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer inner.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inner.URL+"/meta-data", http.StatusFound)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if _, err := c.Avatar(context.Background(), srv.URL+"/avatar.png"); err == nil || hit {
		t.Errorf("redirect followed: err %v, hit %v", err, hit)
	}
}

func TestToCardAvatar(t *testing.T) {
	c := toCard("ABC-1", map[string]json.RawMessage{"assignee": json.RawMessage(`{"accountId":"a1","displayName":"Ada","avatarUrls":{"48x48":"https://a/48.png","24x24":"https://a/24.png"}}`)}, "")
	if c.AvatarURL != "https://a/48.png" || c.Assignee != "Ada" {
		t.Errorf("card = %+v", c)
	}
}
