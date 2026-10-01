package autostart

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fake(t *testing.T, goos string) (System, *[]string) {
	t.Setenv("XDG_CONFIG_HOME", "")
	var ran []string
	return System{OS: goos, Home: t.TempDir(),
		LookPath: func(string) (string, error) { return "/usr/bin/systemctl", nil },
		Run: func(name string, args ...string) error {
			ran = append(ran, name+" "+strings.Join(args, " "))
			return nil
		},
	}, &ran
}

// TestLinux: a user unit under ~/.config/systemd/user, enabled, not started;
// Disable removes it.
func TestLinux(t *testing.T) {
	s, ran := fake(t, "linux")
	argv := []string{"/opt/my apps/laneway", "web", "-no-open", "-addr", "127.0.0.1:8484"}
	if err := s.Enable(argv, "/usr/bin:/bin"); err != nil {
		t.Fatal(err)
	}
	if s.Path() != filepath.Join(s.Home, ".config", "systemd", "user", "laneway-web.service") || !s.Enabled() {
		t.Errorf("path %s enabled %v", s.Path(), s.Enabled())
	}
	b, _ := os.ReadFile(s.Path())
	for _, want := range []string{`ExecStart="/opt/my apps/laneway" "web" "-no-open" "-addr" "127.0.0.1:8484"`, `Environment="PATH=/usr/bin:/bin"`, "WantedBy=default.target"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("unit lacks %s:\n%s", want, b)
		}
	}
	if got := strings.Join(*ran, "; "); got != "systemctl --user daemon-reload; systemctl --user enable laneway-web.service" {
		t.Errorf("ran %s", got)
	}
	*ran = nil
	if err := s.Disable(); err != nil || s.Enabled() {
		t.Fatalf("disable: %v, still %v", err, s.Enabled())
	}
	if got := strings.Join(*ran, "; "); got != "systemctl --user disable laneway-web.service; systemctl --user daemon-reload" {
		t.Errorf("ran %s", got)
	}
}

// TestDarwin: a LaunchAgent that parses as XML, nothing run.
func TestDarwin(t *testing.T) {
	s, ran := fake(t, "darwin")
	if err := s.Enable([]string{"/opt/homebrew/bin/laneway", "web", "-config", "/Users/a&b/c.yaml"}, "/opt/homebrew/bin:/usr/bin"); err != nil {
		t.Fatal(err)
	}
	if s.Path() != filepath.Join(s.Home, "Library", "LaunchAgents", "com.github.cornedor.laneway.web.plist") {
		t.Errorf("path %s", s.Path())
	}
	b, _ := os.ReadFile(s.Path())
	if err := xml.Unmarshal(b, new(struct{})); err != nil {
		t.Errorf("plist: %v\n%s", err, b)
	}
	if !strings.Contains(string(b), "<string>/Users/a&amp;b/c.yaml</string>") || !strings.Contains(string(b), "<key>RunAtLoad</key>") {
		t.Errorf("plist:\n%s", b)
	}
	if len(*ran) != 0 {
		t.Errorf("ran %v", *ran)
	}
	if err := s.Disable(); err != nil || s.Enabled() {
		t.Errorf("disable: %v", err)
	}
}

func TestUnsupported(t *testing.T) {
	s, _ := fake(t, "windows")
	if s.Supported() || !errors.Is(s.Enable([]string{"x"}, ""), ErrUnsupported) {
		t.Error("windows supported")
	}
	s, _ = fake(t, "linux")
	s.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if s.Supported() {
		t.Error("linux without systemctl supported")
	}
}

// TestSystemdQuote: % is a specifier and " ends the word unless escaped.
func TestSystemdQuote(t *testing.T) {
	if got := systemdQuote(`a "b" 100%\`); got != `"a \"b\" 100%%\\"` {
		t.Errorf("quote = %s", got)
	}
}
