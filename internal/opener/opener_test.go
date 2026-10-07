package opener

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opened")
	if err := Open([]string{"touch"}, path); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the command never ran on the target")
}

func TestOpenRefuses(t *testing.T) {
	for _, target := range []string{"smb://evil/share/x.app", "file:///etc/passwd", "vscode://x", "--help", "relative/file", ""} {
		if err := Open([]string{"true"}, target); err == nil {
			t.Errorf("%q opened", target)
		}
	}
	for _, target := range []string{"https://example.com", "HTTP://example.com", "mailto:a@b.c", t.TempDir()} {
		if err := Open([]string{"true"}, target); err != nil {
			t.Errorf("%q: %v", target, err)
		}
	}
}
