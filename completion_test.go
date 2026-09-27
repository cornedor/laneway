package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
)

// TestComplete: commands, flags, site names, formats and cached keys.
func TestComplete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	p := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(p, []byte("sites:\n  club: {base_url: \"https://club.test\"}\n  corp: {base_url: \"https://corp.test\"}\n"), 0o600)
	state, _ := config.SiteStatePath("club")
	_ = os.MkdirAll(filepath.Dir(state), 0o700)
	_ = os.WriteFile(state, []byte(`{"jira_tab:cache:1:Sprint":"{\"Cards\":[{\"Key\":\"ABC-2\"},{\"Key\":\"ABC-10\"},{\"Key\":\"OPS-1\"}]}"}`), 0o600)
	complete := func(args ...string) string {
		var out bytes.Buffer
		completeCmd(args, &out)
		return strings.Join(strings.Fields(out.String()), " ")
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"v"}, "view"},
		{[]string{"list", "-f"}, "-format"},
		{[]string{"list", "-format", ""}, "plain csv json"},
		{[]string{"-config", p, "view", "-site", "c"}, "club corp"},
		{[]string{"-config", p, "-site", "club", "view", "AB"}, "ABC-10 ABC-2"},
		{[]string{"-config", p, "-site", "club", "move", "ABC-2", ""}, ""},
		{[]string{"completion", ""}, "bash zsh fish"},
	} {
		if got := complete(c.args...); got != c.want {
			t.Errorf("%q = %q, want %q", c.args, got, c.want)
		}
	}
	var out, errOut bytes.Buffer
	if code := subcommand([]string{"completion", "zsh"}, "", "", &out, &errOut); code != 0 || !strings.Contains(out.String(), "laneway __complete") {
		t.Errorf("zsh script: %d %q", code, out.String())
	}
}
