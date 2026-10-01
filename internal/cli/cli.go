// Package cli runs the command-line tools laneway leans on (git, gh, glab).
package cli

import (
	"errors"
	"os/exec"
	"strings"
)

// Have is whether bin is on the PATH.
func Have(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

// Error is a failed command's first stderr line, else its error.
func Error(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if line, _, _ := strings.Cut(strings.TrimSpace(string(ee.Stderr)), "\n"); line != "" {
			return line
		}
	}
	return err.Error()
}
