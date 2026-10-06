package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetUI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	orig := "# laneway\njira:\n  base_url: https://x.atlassian.net # site\nui:\n  stale_days: 5 # red after\n  images: off\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name  string
		value any
	}{{"stale_days", 3}, {"images", nil}, {"card_fields", []string{"type", "points"}}} {
		if err := SetUI(link, step.name, step.value); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	want := "# laneway\njira:\n  base_url: https://x.atlassian.net # site\nui:\n  stale_days: 3 # red after\n  card_fields: [type, points]\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by a file")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

func TestSetUINoSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("jira:\n  email: a@b\nui:\n"), 0o600)
	if err := SetUI(path, "branch_template", "{type}/{key}"); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(path)
	if err != nil || c.UI.BranchTemplate != "{type}/{key}" || c.Jira.Email != "a@b" {
		t.Errorf("load = %+v %v", c.UI, err)
	}
}

func TestSetSite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "laneway", "config.yaml")
	if err := SetSite(path, "", JiraConfig{BaseURL: "https://a.atlassian.net", Email: "me@a", APIToken: "t1"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("new file: %v %v", fi, err)
	}
	orig, _ := os.ReadFile(path)
	os.WriteFile(path, append([]byte("# mine\n"), append(orig, "  projects: [ABC] # pinned\nsites:\n  old: {base_url: https://o.atlassian.net, email: me@o, api_token: stale, timeout: 30s}\n"...)...), 0o600)
	// A new site beside jira:, a replaced token on an old one.
	if err := SetSite(path, "club", JiraConfig{BaseURL: "https://club.atlassian.net", Email: "me@club", APIToken: "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := SetSite(path, "old", JiraConfig{BaseURL: "https://o.atlassian.net", Email: "me@o", APIToken: "fresh"}); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Jira.APIToken != "t1" || len(c.Jira.Projects) != 1 || c.Sites["club"].APIToken != "t2" ||
		c.Sites["old"].APIToken != "fresh" || c.Sites["old"].Timeout != "30s" {
		t.Errorf("config = %+v", c)
	}
	got, _ := os.ReadFile(path)
	for _, keep := range []string{"# mine", "# pinned"} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
}

func TestSetProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	orig := "jira:\n  base_url: https://a.atlassian.net\n  projects: [ABC] # mine\nsites:\n  club:\n    base_url: https://club.atlassian.net\n"
	os.WriteFile(path, []byte(orig), 0o600)
	if err := SetProjects(path, "", []string{"XYZ", "ABC"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProjects(path, "club", []string{"CL"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "jira:\n  base_url: https://a.atlassian.net\n  projects: [XYZ, ABC] # mine\nsites:\n  club:\n    base_url: https://club.atlassian.net\n    projects: [CL]\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if err := SetProjects(path, "club", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), "CL") {
		t.Errorf("projects kept:\n%s", got)
	}
}

// TestSetSiteNoToken: no token (JIRA_API_TOKEN stands in) leaves the key out.
func TestSetSiteNoToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SetSite(path, "", JiraConfig{BaseURL: "https://a.atlassian.net", Email: "me@a"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), "api_token") {
		t.Errorf("file =\n%s", got)
	}
}

// TestSetSiteTightensMode: a token written into a world-readable config
// makes it owner-only; a UI option leaves the mode alone.
func TestSetSiteTightensMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("ui:\n  theme: dark\n"), 0o644)
	os.Chmod(path, 0o644)
	if err := SetUI(path, "theme", "light"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("after SetUI mode %v", fi.Mode().Perm())
	}
	if err := SetSite(path, "", JiraConfig{BaseURL: "https://a.atlassian.net", Email: "me@a", APIToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("after SetSite mode %v", fi.Mode().Perm())
	}
}

// TestSetUIKeepsOddFiles: a comment-only file keeps its notes, later YAML
// documents stay, and a bare "---" is an empty config.
func TestSetUIKeepsOddFiles(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"# my notes\n# more\n", "# my notes\n# more\nui:\n  theme: light\n"},
		{"ui:\n  theme: dark\n---\nother: 1\n", "ui:\n  theme: light\n---\nother: 1\n"},
		{"---\n", "ui:\n  theme: light\n"},
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		os.WriteFile(path, []byte(tc.in), 0o600)
		if err := SetUI(path, "theme", "light"); err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got, _ := os.ReadFile(path); string(got) != tc.want {
			t.Errorf("%q →\n%s", tc.in, got)
		}
	}
}
