package gitlab

import (
	"strings"
	"testing"
)

// TestSignIn: glab's command, its install link when it is missing, and the
// token page with scope api; a rejected token says so.
func TestSignIn(t *testing.T) {
	defer func(f func() bool) { lookGlab = f }(lookGlab)
	lookGlab = func() bool { return false }
	si := SignInFor("git.example.com")
	text := strings.Join(si.Lines(), "\n")
	for _, want := range []string{"No GitLab token for git.example.com", "glab auth login --hostname git.example.com", GlabInstall,
		"https://git.example.com/-/user_settings/personal_access_tokens?name=laneway&scopes=api"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	lookGlab = func() bool { return true }
	si = SignInFor("git.example.com")
	si.Rejected = true
	if text := strings.Join(si.Lines(), "\n"); strings.Contains(text, GlabInstall) || !strings.Contains(text, "rejected the token") {
		t.Errorf("glab installed, rejected:\n%s", text)
	}
	if !IsMRLink("https://any.host/g/p/-/merge_requests/3") || IsMRLink("https://github.com/o/r/pull/3") {
		t.Error("IsMRLink")
	}
	// A host that would break out of the glab command is no host.
	for _, link := range []string{"https://x;reboot.example/g/p/-/merge_requests/1", "https://x$(id).example/g/p/-/merge_requests/1",
		"https://a'b/g/p/-/merge_requests/1", "https://x\u009b.example/g/p/-/merge_requests/1"} {
		if IsMRLink(link) {
			t.Errorf("IsMRLink(%q)", link)
		}
	}
}

// TestSitesForget: a host without a token is asked glab again once
// forgotten, after glab auth login.
func TestSitesForget(t *testing.T) {
	s := NewSites(nil)
	tok := ""
	s.glab = func(string) string { return tok }
	link := "https://git.example.com/g/p/-/merge_requests/1"
	if s.For(link) != nil {
		t.Fatal("no token yet")
	}
	tok = "fresh"
	if s.For(link) != nil {
		t.Error("the miss is cached until forgotten")
	}
	s.Forget(link)
	if c := s.For(link); c == nil || c.token != "fresh" {
		t.Errorf("after Forget: %+v", c)
	}
}
