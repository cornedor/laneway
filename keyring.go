package main

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/cornedor/laneway/internal/i18n"
)

// The OS keyring for setup: secret-tool (libsecret) on Linux, security
// (Keychain) on macOS. The token goes in once; the config keeps only the
// command that reads it back (api_token_cmd).

// lookPath is exec.LookPath; tests swap it.
var lookPath = exec.LookPath

// keyring is the OS keyring for one site and account: store reads
// input(token) on its stdin, lookup prints the token back.
type keyring struct {
	store, lookup []string
	input         func(token string) (string, error)
}

// keyringFor is the keyring for the site and account, nil without a
// keyring tool.
func keyringFor(baseURL, email string) *keyring {
	return keyringOn(runtime.GOOS, baseURL, email)
}

func keyringOn(goos, baseURL, email string) *keyring {
	service := "laneway " + strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	switch {
	case goos == "darwin":
		if _, err := lookPath("security"); err == nil {
			// -w last would prompt, and without a terminal it stores ""; -i
			// takes the command on stdin, so the token stays out of ps.
			return &keyring{
				store:  []string{"security", "-i"},
				lookup: []string{"security", "find-generic-password", "-a", email, "-s", service, "-w"},
				input: func(token string) (string, error) {
					args := []string{"add-generic-password", "-U", "-a", email, "-s", service, "-w", token}
					for i, a := range args {
						if strings.ContainsAny(a, "\"\\\n") {
							return "", errorString(i18n.T("security takes no \", \\ or line break here"))
						}
						args[i] = `"` + a + `"`
					}
					return strings.Join(args, " ") + "\n", nil
				},
			}
		}
	default:
		if _, err := lookPath("secret-tool"); err == nil {
			return &keyring{
				store:  []string{"secret-tool", "store", "--label=" + service, "service", service, "account", email},
				lookup: []string{"secret-tool", "lookup", "service", service, "account", email},
				input:  func(token string) (string, error) { return token, nil },
			}
		}
	}
	return nil
}

// runKeyring runs a keyring command with input on its stdin, and answers
// what it printed; tests swap it.
var runKeyring = func(cmd []string, input string) (string, error) {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin = strings.NewReader(input)
	var out, errOut strings.Builder
	c.Stdout, c.Stderr = &out, &errOut
	if err := c.Run(); err != nil {
		if s := strings.TrimSpace(errOut.String() + out.String()); s != "" {
			return "", errorString(s)
		}
		return "", err
	}
	return out.String(), nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

// put stores token in the keyring and reads it back: a keyring that
// answers yes but keeps something else is refused.
func (k *keyring) put(token string) error {
	in, err := k.input(token)
	if err != nil {
		return err
	}
	if _, err := runKeyring(k.store, in); err != nil {
		return err
	}
	got, err := runKeyring(k.lookup, "")
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) != token {
		return errorString(i18n.T("it did not keep the token"))
	}
	return nil
}
