// Package autostart starts `laneway web` when the user logs in, without
// root: a systemd user unit on Linux, a LaunchAgent on macOS.
package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// unit is the systemd unit's file name, label the LaunchAgent's.
const (
	unit  = "laneway-web.service"
	label = "com.github.cornedor.laneway.web"
)

// ErrUnsupported is a system without systemd's user manager or launchd.
var ErrUnsupported = errors.New("starting at login needs systemd (Linux) or launchd (macOS)")

// System is where and how the service is installed; tests swap its parts.
type System struct {
	OS   string // runtime.GOOS
	Home string
	// Run runs a service manager command (systemctl --user …).
	Run      func(name string, args ...string) error
	LookPath func(string) (string, error)
}

// Default is this machine.
func Default() System {
	home, _ := os.UserHomeDir()
	return System{OS: runtime.GOOS, Home: home, LookPath: exec.LookPath, Run: func(name string, args ...string) error {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil && len(bytes.TrimSpace(out)) > 0 {
			return fmt.Errorf("%s: %s", name, bytes.TrimSpace(out))
		}
		return err
	}}
}

// Supported is whether the system has a service manager this can use.
func (s System) Supported() bool {
	switch s.OS {
	case "linux":
		_, err := s.LookPath("systemctl")
		return err == nil
	case "darwin":
		return true
	}
	return false
}

// Path is the file the service lives in.
func (s System) Path() string {
	if s.OS == "darwin" {
		return filepath.Join(s.Home, "Library", "LaunchAgents", label+".plist")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(s.Home, ".config")
	}
	return filepath.Join(config, "systemd", "user", unit)
}

// Enabled is whether the service is installed.
func (s System) Enabled() bool {
	_, err := os.Stat(s.Path())
	return err == nil
}

// Enable installs the service to run argv (the binary first) at the next
// login, with PATH as the command's search path. It does not start it now:
// the laneway asking is the one on the address.
func (s System) Enable(argv []string, path string) error {
	if !s.Supported() {
		return ErrUnsupported
	}
	if len(argv) == 0 {
		return errors.New("no command to start")
	}
	p := s.Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	text := Unit(argv, path)
	if s.OS == "darwin" {
		text = Plist(argv, path)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return err
	}
	if s.OS == "linux" {
		if err := s.Run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return s.Run("systemctl", "--user", "enable", unit)
	}
	return nil // launchd loads LaunchAgents at login
}

// Disable removes the service. A running one is left running: it may be
// the laneway that asked.
func (s System) Disable() error {
	if !s.Supported() {
		return ErrUnsupported
	}
	if s.OS == "linux" && s.Enabled() {
		if err := s.Run("systemctl", "--user", "disable", unit); err != nil {
			return err
		}
	}
	if err := os.Remove(s.Path()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if s.OS == "linux" {
		return s.Run("systemctl", "--user", "daemon-reload")
	}
	return nil
}

// Unit is the systemd user unit running argv.
func Unit(argv []string, path string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = systemdQuote(a)
	}
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=laneway web\nAfter=network-online.target\n\n[Service]\n")
	b.WriteString("ExecStart=" + strings.Join(q, " ") + "\n")
	if path != "" {
		b.WriteString("Environment=" + systemdQuote("PATH="+path) + "\n")
	}
	b.WriteString("Restart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// systemdQuote quotes an ExecStart word: double quotes, \ and " escaped,
// % doubled (a specifier otherwise).
func systemdQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s)
	return `"` + s + `"`
}

// Plist is the LaunchAgent running argv at login, again when it fails.
func Plist(argv []string, path string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + label + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range argv {
		b.WriteString("\t\t<string>" + html.EscapeString(a) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	if path != "" {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>PATH</key>\n\t\t<string>" + html.EscapeString(path) + "</string>\n\t</dict>\n")
	}
	b.WriteString(`	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
</dict>
</plist>
`)
	return b.String()
}
