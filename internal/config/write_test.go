package config

import (
	"os"
	"path/filepath"
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
