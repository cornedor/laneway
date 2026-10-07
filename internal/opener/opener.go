// Package opener launches the platform's default handler for a URL or
// local file path — the desktop equivalent of double-clicking it.
package opener

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Open hands target (a URL or a local file path) to command (ui.open) when
// given, else the OS default handler. The launcher forks and returns
// immediately; we don't wait for the viewer/browser process to exit. On
// every platform the chosen command accepts both URLs and filesystem paths.
func Open(command []string, target string) error {
	if !allowed(target) {
		return errors.New("only http(s) and mailto links and local files open")
	}
	if len(command) > 0 {
		return exec.Command(command[0], append(command[1:], target)...).Start()
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// allowed: an http(s) or mailto URL, or an absolute path. Links come from
// Jira and GitLab content, so smb:, file:, custom schemes and a leading -
// (an option to the launcher) stay out.
func allowed(target string) bool {
	l := strings.ToLower(target)
	return strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "mailto:") || filepath.IsAbs(target)
}
