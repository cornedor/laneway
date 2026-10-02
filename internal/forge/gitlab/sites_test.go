package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cornedor/laneway/internal/forge"
)

// TestSites: a configured token wins; a configured instance without one,
// or a host none names, takes glab's, asked once; no token anywhere is nil.
func TestSites(t *testing.T) {
	s := NewSites([]Config{
		{BaseURL: "https://git.example.com", Token: "cfg"},
		{BaseURL: "code.example.org/gitlab"},
	})
	asked := map[string]int{}
	s.glab = func(host string) string {
		asked[host]++
		if host == "nowhere.example" {
			return ""
		}
		return "glab-" + host
	}
	for range 2 {
		if c := s.For("https://git.example.com/g/p/-/merge_requests/1"); c == nil || c.token != "cfg" {
			t.Errorf("configured: %+v", c)
		}
		if c := s.For("https://code.example.org/gitlab/g/p/-/merge_requests/1"); c == nil || c.token != "glab-code.example.org" || c.BaseURL() != "https://code.example.org/gitlab" {
			t.Errorf("configured without a token: %+v", c)
		}
		if c := s.For("https://gitlab.com/g/p/-/merge_requests/1"); c == nil || c.token != "glab-gitlab.com" {
			t.Errorf("glab only: %+v", c)
		}
		if c := s.For("https://nowhere.example/g/p/-/merge_requests/1"); c != nil {
			t.Errorf("no token: %+v", c)
		}
	}
	if asked["git.example.com"] != 0 || asked["gitlab.com"] != 1 || asked["nowhere.example"] != 1 {
		t.Errorf("glab asked %v, want once per host without a token", asked)
	}
}

// TestSitesCheck: every configured host, then glab's others, each with its
// token's source and account; a host without a token says so.
func TestSitesCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"username": "ada"}`))
	}))
	defer srv.Close()
	host := forge.HostOf(srv.URL)
	s := NewSites([]Config{{BaseURL: srv.URL, Token: "cfg"}, {BaseURL: "https://none.example"}})
	s.glab = func(string) string { return "" }
	s.glabHosts = func() []string { return []string{host, "glab.example"} }
	got := s.Check(context.Background())
	if len(got) != 3 || got[0].Host != host || got[0].From != "config" || got[0].User.Username != "ada" || got[0].Err != nil {
		t.Fatalf("configured: %+v", got)
	}
	for _, st := range got[1:] {
		if !errors.Is(st.Err, forge.ErrNotConfigured) || st.From != "" {
			t.Errorf("no token: %+v", st)
		}
	}
	if s := got[0].Summary(); s != "signed in as ada, token from config" {
		t.Errorf("summary %q", s)
	}
	if got[2].Host != "glab.example" {
		t.Errorf("glab's hosts after the configured: %+v", got)
	}
}

// TestSitesSearch: every instance with a token is searched; gitlab.com
// only when configured.
func TestSitesSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"iid": 1, "title": "ABC-1 fix", "web_url": "http://` + r.Host + `/g/p/-/merge_requests/1"}]`))
	}))
	defer srv.Close()
	s := NewSites([]Config{{BaseURL: srv.URL, Token: "tok"}})
	var asked []string
	s.glab = func(h string) string { asked = append(asked, h); return "tok" }
	s.glabHosts = func() []string { return []string{"gitlab.com"} }
	if got := s.Search(context.Background(), "ABC-1"); len(got) != 1 || got[0].Number != 1 {
		t.Errorf("Search = %+v", got)
	}
	if len(asked) != 0 {
		t.Errorf("asked glab for %v: gitlab.com unconfigured is not searched", asked)
	}
}
