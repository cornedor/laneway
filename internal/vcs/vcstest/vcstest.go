// Package vcstest makes jj repositories for tests.
package vcstest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/vcs"
)

// JJRepo is a colocated jj repository with a bare origin, main pushed to
// it: trunk() is main@origin. jj runs a jj command in a directory of it.
// It skips without jj.
func JJRepo(t *testing.T) (repo, origin string, jj func(dir string, args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("no jj")
	}
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "jj.toml")
	if err := os.WriteFile(cfg, []byte("user.name = \"T\"\nuser.email = \"t@example.test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JJ_CONFIG", cfg)
	jj = func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("jj", append([]string{"--no-pager", "--color=never", "-R", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin = filepath.Join(tmp, "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", origin).CombinedOutput(); err != nil {
		t.Skip("git:", err, string(out))
	}
	repo = filepath.Join(tmp, "repo")
	if out, err := exec.Command("jj", "git", "init", "--colocate", repo).CombinedOutput(); err != nil {
		t.Fatalf("jj git init: %v\n%s", err, out)
	}
	jj(repo, "git", "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(repo, "a"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	jj(repo, "commit", "-m", "init")
	jj(repo, "bookmark", "create", "main", "-r", "@-")
	if _, err := vcs.Push(context.Background(), repo, "main"); err != nil {
		t.Fatal("push a new bookmark:", err)
	}
	return repo, origin, jj
}
