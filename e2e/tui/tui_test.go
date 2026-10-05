// Package tui drives `laneway -demo` in a real terminal: a tmux server of
// its own, keys sent as a person types them, the screen read back. It
// skips without tmux.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var laneway string // the binary under test, built by TestMain

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Println("skip: no tmux")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "laneway-e2e-")
	if err != nil {
		panic(err)
	}
	laneway = filepath.Join(dir, "laneway")
	if out, err := exec.Command("go", "build", "-o", laneway, "../..").CombinedOutput(); err != nil {
		fmt.Printf("build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// term is one laneway -demo in a tmux pane.
type term struct {
	t               *testing.T
	sock, unhandled string
}

const cols, rows = 140, 42

// start runs laneway -demo in a fresh home; cleanup fails the test on a
// request the demo couldn't answer.
func start(t *testing.T) *term {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e")
	}
	home := t.TempDir()
	// A named socket: a path under the temp dir can pass the 104 bytes a
	// socket's may have on macOS.
	s := &term{t: t, sock: fmt.Sprintf("laneway-e2e-%d-%s", os.Getpid(), t.Name()), unhandled: filepath.Join(home, "unhandled.txt")}
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home + "/config", "XDG_STATE_HOME=" + home + "/state",
		"XDG_CACHE_HOME=" + home + "/cache", "TERM=xterm-256color", "LANEWAY_DEMO_UNHANDLED=" + s.unhandled}
	// escape-time 0: tmux passes a lone esc at once instead of waiting to
	// see whether a key follows it.
	s.tmux("-f", "/dev/null", "new-session", "-d", "-s", "e2e", "-x", fmt.Sprint(cols), "-y", fmt.Sprint(rows),
		"env "+strings.Join(env, " ")+" "+laneway+" -demo", ";", "set", "-s", "escape-time", "0")
	path := strings.TrimSpace(s.tmux("display", "-p", "#{socket_path}"))
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", s.sock, "kill-server").Run()
		os.Remove(path) // tmux leaves it
		if b, _ := os.ReadFile(s.unhandled); len(b) > 0 {
			t.Errorf("requests the demo could not answer:\n%s", b)
		}
	})
	s.wait("DEMO-5")
	return s
}

func (s *term) tmux(args ...string) string {
	s.t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", s.sock}, args...)...).CombinedOutput()
	if err != nil {
		s.t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// keys presses keys by tmux's names ("Enter", "Escape", "C-s", "L").
func (s *term) keys(keys ...string) {
	s.t.Helper()
	for _, k := range keys {
		s.tmux("send-keys", "-t", "e2e", k)
		if k == "Escape" {
			// A key right behind esc would read as alt+key.
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// typ types text as it is.
func (s *term) typ(text string) {
	s.t.Helper()
	s.tmux("send-keys", "-t", "e2e", "-l", text)
}

func (s *term) screen() string {
	s.t.Helper()
	return s.tmux("capture-pane", "-p", "-t", "e2e")
}

// wait returns the screen once it shows want, and fails the test if it
// doesn't within 10s.
func (s *term) wait(want string) string {
	s.t.Helper()
	return s.until(func(scr string) bool { return strings.Contains(scr, want) }, "no "+want)
}

// gone returns the screen once it no longer shows what.
func (s *term) gone(what string) string {
	s.t.Helper()
	return s.until(func(scr string) bool { return !strings.Contains(scr, what) }, "still "+what)
}

func (s *term) until(ok func(string) bool, why string) string {
	s.t.Helper()
	var scr string
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if scr = s.screen(); ok(scr) {
			return scr
		}
	}
	s.t.Fatalf("%s on the screen:\n%s", why, scr)
	return ""
}

// running is whether laneway is still the pane's process.
func (s *term) running() bool {
	out, err := exec.Command("tmux", "-L", s.sock, "list-panes", "-t", "e2e", "-F", "#{pane_dead}").Output()
	return err == nil && strings.TrimSpace(string(out)) == "0"
}
