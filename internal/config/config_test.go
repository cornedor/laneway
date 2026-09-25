package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatePathMigratesOldState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	d, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(d, "jiratui", "state.json")
	_ = os.MkdirAll(filepath.Dir(old), 0o700)
	_ = os.WriteFile(old, []byte(`{"meta":{}}`), 0o600)
	p, err := StatePath()
	if err != nil || p != filepath.Join(d, "laneway", "state.json") {
		t.Fatalf("path = %q, %v", p, err)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != `{"meta":{}}` {
		t.Errorf("migrated = %q, %v", b, err)
	}
}

func TestLoadFallsBackToJiratui(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	d, _ := os.UserConfigDir()
	p := filepath.Join(d, "jiratui", "config.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("jira:\n  base_url: https://x.test\n"), 0o600)
	c, got, err := Load("")
	if err != nil || got != p || c.Jira.BaseURL != "https://x.test" {
		t.Errorf("Load = %+v %q %v", c.Jira, got, err)
	}
}
