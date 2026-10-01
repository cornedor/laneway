package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// server is the laneway web this app shows: one already running (started
// at login, say) or one it starts and stops.
type server struct {
	addr string
	cmd  *exec.Cmd
}

func (s *server) start() error {
	if s.running() {
		return nil
	}
	exe, err := lanewayPath()
	if err != nil {
		return err
	}
	logPath := filepath.Join(os.TempDir(), "laneway-desktop.log")
	if dir, err := os.UserCacheDir(); err == nil && os.MkdirAll(filepath.Join(dir, "laneway"), 0o700) == nil {
		logPath = filepath.Join(dir, "laneway", "desktop.log")
	}
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	s.cmd = exec.Command(exe, "web", "-no-open", "-addr", s.addr)
	s.cmd.Stdout, s.cmd.Stderr = logf, logf
	s.cmd.Env = append(os.Environ(), "PATH="+loginPath())
	if err := s.cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait(); logf.Close() }()
	for range 100 {
		select {
		case err := <-exited:
			s.cmd = nil
			return fmt.Errorf("laneway web stopped (%v); see %s", err, logPath)
		case <-time.After(200 * time.Millisecond):
		}
		if s.running() {
			return nil
		}
	}
	return fmt.Errorf("laneway web did not answer on %s; see %s", s.addr, logPath)
}

// stop ends the laneway web this app started; one it found keeps running.
func (s *server) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	time.AfterFunc(3*time.Second, func() { _ = s.cmd.Process.Kill() })
}

// running is whether a laneway web answers on addr.
func (s *server) running() bool {
	c := http.Client{Timeout: time.Second}
	resp, err := c.Get("http://" + s.addr + "/api/session")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&v) == nil && v.Version != ""
}

// lanewayPath is the laneway binary in the app bundle, else on PATH.
func lanewayPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "laneway")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("laneway"); err == nil {
		return p, nil
	}
	return "", errors.New("laneway is not next to this app nor on PATH")
}

// loginPath is the PATH of the user's shell: an app started from the Finder
// gets a bare one, without brew or the tools ui.actions and agents run.
func loginPath() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return os.Getenv("PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, sh, "-l", "-i", "-c", `printf '\n@@%s@@' "$PATH"`).Output()
	if _, rest, ok := bytes.Cut(out, []byte("\n@@")); err == nil && ok {
		if p, _, ok := bytes.Cut(rest, []byte("@@")); ok && len(p) > 0 {
			return string(p)
		}
	}
	return os.Getenv("PATH")
}
