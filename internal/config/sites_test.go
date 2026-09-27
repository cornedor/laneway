package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSites(t *testing.T) {
	var c Config
	err := yaml.Unmarshal([]byte(`
jira: {base_url: https://a.atlassian.net, email: a@x, api_token: t}
sites:
  work: {base_url: https://w.atlassian.net, email: w@x, api_token: u}
  club: {base_url: https://c.atlassian.net, email: c@x, api_token: v}
`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SiteNames(); !slices.Equal(got, []string{"", "club", "work"}) {
		t.Errorf("names = %q", got)
	}
	if j, err := c.Site("work"); err != nil || j.BaseURL != "https://w.atlassian.net" {
		t.Errorf("work = %+v, %v", j, err)
	}
	if j, _ := c.Site(""); j.BaseURL != "https://a.atlassian.net" {
		t.Errorf("default = %+v", j)
	}
	if _, err := c.Site("nope"); err == nil {
		t.Error("an unknown site should fail")
	}
}

func TestSiteStatePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	def, _ := SiteStatePath("")
	work, _ := SiteStatePath("work")
	if filepath.Base(def) != "state.json" || filepath.Base(work) != "state-work.json" || filepath.Dir(def) != filepath.Dir(work) {
		t.Errorf("paths %s, %s", def, work)
	}
	if !strings.HasSuffix(filepath.Dir(work), "laneway") {
		t.Errorf("dir = %s", filepath.Dir(work))
	}
}

func TestBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"acme":                  "https://acme.atlassian.net",
		" acme.atlassian.net/ ": "https://acme.atlassian.net",
		"https://acme.atlassian.net/jira/software/projects/ABC/boards/1?x=1": "https://acme.atlassian.net",
		"jira.example.com/jira": "https://jira.example.com/jira",
		"http://localhost:8080": "http://localhost:8080",
	} {
		if got, err := BaseURL(in); err != nil || got != want {
			t.Errorf("BaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x.org", "https://"} {
		if _, err := BaseURL(bad); err == nil {
			t.Errorf("BaseURL(%q) accepted", bad)
		}
	}
}

func TestSiteFor(t *testing.T) {
	c := Config{Jira: JiraConfig{BaseURL: "https://a.atlassian.net/"}, Sites: map[string]JiraConfig{"club": {BaseURL: "https://Club.atlassian.net"}}}
	if name, ok := c.SiteFor("https://a.atlassian.net"); !ok || name != "" {
		t.Errorf("jira: = %q %v", name, ok)
	}
	if name, ok := c.SiteFor("https://club.atlassian.net"); !ok || name != "club" {
		t.Errorf("club = %q %v", name, ok)
	}
	if _, ok := c.SiteFor("https://new.atlassian.net"); ok {
		t.Error("new site found")
	}
	if SiteName("https://club.atlassian.net") != "club" || !ValidSiteName("club-2_b") || ValidSiteName("my club") || ValidSiteName("Club") {
		t.Error("site names")
	}
}

func TestLastSite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	names := []string{"", "club", "work"}
	if got := LastSite(names); got != "" {
		t.Errorf("nothing picked yet = %q", got)
	}
	if err := SetLastSite("club"); err != nil {
		t.Fatal(err)
	}
	if got := LastSite(names); got != "club" {
		t.Errorf("after picking club = %q", got)
	}
	if got := LastSite([]string{"", "work"}); got != "" {
		t.Errorf("a site since removed = %q, want the default", got)
	}
	if err := SetLastSite(""); err != nil || LastSite(names) != "" {
		t.Errorf("picking jira: again = %q, %v", LastSite(names), err)
	}
}
