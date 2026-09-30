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
	got = "unset"
	if _, err := other.Avatar(context.Background(), srv.URL+"/avatar.png"); err == nil || got != "unset" {
		t.Errorf("another host was fetched (auth %q)", got)
	}
	if _, err := other.Avatar(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("a file url was fetched")
	}
}

// TestAvatarHosts: only the instance and the fixed avatar prefixes, and the
// outgoing URL is rebuilt from a constant prefix.
func TestAvatarHosts(t *testing.T) {
	c := New(Config{BaseURL: "https://x.atlassian.net", Email: "me@x.test", APIToken: "tok"})
	for raw, want := range map[string]string{
		"https://x.atlassian.net/secure/useravatar":                                    "https://x.atlassian.net/secure/useravatar",
		"https://secure.gravatar.com/avatar/abc?d=mm":                                  "https://secure.gravatar.com/avatar/abc?d=mm",
		"https://avatar-management--avatars.us-west-2.prod.public.atl-paas.net/a/b/48": "https://avatar-management--avatars.us-west-2.prod.public.atl-paas.net/a/b/48",
		"https://x.atlassian.net/a%2Bb?x=1&y=a+b":                                      "https://x.atlassian.net/a+b?x=1&y=a+b",
		"http://secure.gravatar.com/avatar/abc":                                        "",
		"https://atl-paas.net.evil.test/a":                                             "",
		"https://evilatl-paas.net/a":                                                   "",
		"https://secure.gravatar.com:8443/avatar/abc":                                  "",
		"https://secure.gravatar.com@evil.test/avatar":                                 "",
		"https://x.atlassian.net@evil.test/a":                                          "",
		"https://user:pw@secure.gravatar.com/a":                                        "",
		"https://SECURE.gravatar.com/avatar/abc":                                       "",
		"https://secure.gravatar.com/../admin":                                         "",
		"https://secure.gravatar.com/a/%2e%2e/b":                                       "",
		"https://secure.gravatar.com/a//b":                                             "",
		"https://secure.gravatar.com/a\\b":                                             "",
		"https://secure.gravatar.com/a%5Cb":                                            "",
		"https://secure.gravatar.com/a%2Fb":                                            "",
		"https://secure.gravatar.com/a%0d%0a":                                          "",
		"https://secure.gravatar.com/\uff0e\uff0e/b":                                   "",
		"https://secure.gravatar.com/a#frag":                                           "",
		"https://secure.gravatar.com.evil.test/a":                                      "",
		"http://169.254.169.254/latest/meta-data":                                      "",
		"http://localhost:8080/admin":                                                  "",
	} {
		got, _, ok := c.avatarTarget(raw)
		if want == "" && ok || want != "" && (!ok || got != want) {
			t.Errorf("avatarTarget(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	if _, own, _ := c.avatarTarget("https://secure.gravatar.com/a"); own {
		t.Error("gravatar is not the instance")
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
