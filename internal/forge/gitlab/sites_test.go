package gitlab

import "testing"

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
