package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
)

// Tests run without a keyring unless one asks for it.
func init() { lookPath = func(string) (string, error) { return "", exec.ErrNotFound } }

// TestSetupKeyring: with a keyring, setup stores the token there and the
// config gets the command that reads it back, not the token.
func TestSetupKeyring(t *testing.T) {
	oldLook, oldRun := lookPath, runKeyring
	t.Cleanup(func() { lookPath, runKeyring = oldLook, oldRun })
	lookPath = func(string) (string, error) { return "/usr/bin/tool", nil }
	var stored string
	runKeyring = func(cmd []string, input string) error { stored = input; return nil }
	path := filepath.Join(t.TempDir(), "config.yaml")
	p := prompter{in: bufio.NewReader(strings.NewReader("acme\nme@acme.test\n\n")), out: io.Discard,
		secret: func() (string, error) { return "tok-9", nil }, open: func(string) error { return nil }}
	if _, err := setup(context.Background(), path, p, func(context.Context, config.JiraConfig) (string, error) { return "Me", nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if stored != "tok-9" || strings.Contains(string(b), "tok-9") || !strings.Contains(string(b), "api_token_cmd:") {
		t.Errorf("stored %q, config:\n%s", stored, b)
	}
}
