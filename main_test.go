package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSubcommand: global flags before a subcommand reach it; an unknown one
// is an error, not the board.
func TestSubcommand(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(p, []byte("sites:\n  club: {base_url: \"https://club.test\"}\nrules:\n  - {name: r, actions: [{type: log}]}\n"), 0o600)
	var out, errOut bytes.Buffer
	if code := subcommand([]string{"rules", "list"}, p, "club", &out, &errOut); code != 0 || !strings.Contains(out.String(), p+": 1 rules") {
		t.Errorf("rules list: exit %d, %q %q", code, out.String(), errOut.String())
	}
	errOut.Reset()
	if code := subcommand([]string{"rules", "list"}, p, "nope", &out, &errOut); code != 1 || !strings.Contains(errOut.String(), `no site "nope"`) {
		t.Errorf("-site should reach rules: exit %d, %q", code, errOut.String())
	}
	errOut.Reset()
	if code := subcommand([]string{"setpu"}, p, "", &out, &errOut); code != 2 || !strings.Contains(errOut.String(), `unknown command "setpu"`) {
		t.Errorf("setpu: exit %d, %q", code, errOut.String())
	}
}
