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
	runKeyring = func(cmd []string, input string) (string, error) {
		if cmd[1] == "lookup" {
			return stored + "\n", nil
		}
		stored = input
		return "", nil
	}
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

// TestSetupKeyringKeepsNothing: a keyring that says yes but reads back
// something else (macOS security stored "" without a terminal) leaves the
// token in the file.
func TestSetupKeyringKeepsNothing(t *testing.T) {
	oldLook, oldRun := lookPath, runKeyring
	t.Cleanup(func() { lookPath, runKeyring = oldLook, oldRun })
	lookPath = func(string) (string, error) { return "/usr/bin/tool", nil }
	runKeyring = func([]string, string) (string, error) { return "\n", nil }
	path := filepath.Join(t.TempDir(), "config.yaml")
	p := prompter{in: bufio.NewReader(strings.NewReader("acme\nme@acme.test\n\n")), out: io.Discard,
		secret: func() (string, error) { return "tok-9", nil }, open: func(string) error { return nil }}
	if _, err := setup(context.Background(), path, p, func(context.Context, config.JiraConfig) (string, error) { return "Me", nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "tok-9") || strings.Contains(string(b), "api_token_cmd:") {
		t.Errorf("config:\n%s", b)
	}
}

// TestKeyringMacInput: on macOS the token goes to security -i as a quoted
// command on stdin, never on the command line.
func TestKeyringMacInput(t *testing.T) {
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(string) (string, error) { return "/usr/bin/security", nil }
	k := keyringOn("darwin", "https://acme.atlassian.net", "me@acme.test")
	if strings.Join(k.store, " ") != "security -i" {
		t.Errorf("store = %q", k.store)
	}
	in, err := k.input("ATATT3x-_=")
	want := `"add-generic-password" "-U" "-a" "me@acme.test" "-s" "laneway acme.atlassian.net" "-w" "ATATT3x-_="` + "\n"
	if err != nil || in != want {
		t.Errorf("input = %q, %v; want %q", in, err, want)
	}
	if _, err := k.input(`to"k`); err == nil {
		t.Error("a quote in the token went through")
	}
}
