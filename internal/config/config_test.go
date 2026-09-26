package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestMatterboxRulesIgnored: matterbox's rules: are chat rules, not ours.
func TestMatterboxRulesIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	d, _ := os.UserConfigDir()
	p := filepath.Join(d, "matterbox", "config.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("jira:\n  base_url: https://x.test\nrules:\n  - match: {channel: Ops, frequency: {count: 3}}\n    actions: [{type: send, text: hi}]\n"), 0o600)
	c, _, err := Load("")
	if err != nil || c.Jira.BaseURL != "https://x.test" || c.Rules != nil {
		t.Errorf("Load = %+v rules %v, %v", c.Jira, c.Rules, err)
	}
}

func TestRequestTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "45s": 45 * time.Second, " 1m ": time.Minute} {
		if got, err := (JiraConfig{Timeout: in}).RequestTimeout(); err != nil || got != want {
			t.Errorf("%q = %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"soon", "10ms"} {
		if _, err := (JiraConfig{Timeout: bad}).RequestTimeout(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
