package main

import (
	"os/exec"
	"runtime"
	"strings"
)

// The OS keyring for setup: secret-tool (libsecret) on Linux, security
// (Keychain) on macOS. The token goes in once; the config keeps only the
// command that reads it back (api_token_cmd).

// lookPath is exec.LookPath; tests swap it.
var lookPath = exec.LookPath

// keyringFor is the command storing a token for the site and account
// (the token on its stdin) and the one printing it back; nil without a
// keyring tool.
func keyringFor(baseURL, email string) (store, lookup []string) {
	service := "laneway " + strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	switch {
	case runtime.GOOS == "darwin":
		if _, err := lookPath("security"); err == nil {
			// -w last reads the password from stdin.
			return []string{"security", "add-generic-password", "-U", "-a", email, "-s", service, "-w"},
				[]string{"security", "find-generic-password", "-a", email, "-s", service, "-w"}
		}
	default:
		if _, err := lookPath("secret-tool"); err == nil {
			return []string{"secret-tool", "store", "--label=" + service, "service", service, "account", email},
				[]string{"secret-tool", "lookup", "service", service, "account", email}
		}
	}
	return nil, nil
}

// runKeyring runs a keyring command with input on its stdin; tests swap it.
var runKeyring = func(cmd []string, input string) error {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin = strings.NewReader(input)
	if out, err := c.CombinedOutput(); err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return errorString(s)
		}
		return err
	}
	return nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

// keyringStore puts token in the keyring with store.
func keyringStore(store []string, token string) error {
	return runKeyring(store, token)
}
