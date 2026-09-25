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
