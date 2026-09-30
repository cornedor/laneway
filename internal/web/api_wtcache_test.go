package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWorktreeCachePerSite(t *testing.T) {
	mk := func(branch string) string {
		d := t.TempDir()
		run := func(args ...string) {
			c := exec.Command("git", append([]string{"-C", d, "-c", "user.email=a@b.c", "-c", "user.name=a"}, args...)...)
			if out, err := c.CombinedOutput(); err != nil {
				t.Skip("git unavailable:", string(out))
			}
		}
		run("init", "-q")
		run("commit", "-q", "--allow-empty", "-m", "x")
		run("worktree", "add", "-q", "-b", branch, filepath.Join(d, "wt"))
		_ = os.MkdirAll(d, 0o755)
		return d
	}
	a, b := mk("issue/AAA-1-x"), mk("issue/BBB-2-y")
	forgetWorktrees()
	sa := &Server{opt: Options{Site: "a"}}
	sa.opt.Jira.Repos = map[string]string{"r": a}
	sb := &Server{opt: Options{Site: "b"}}
	sb.opt.Jira.Repos = map[string]string{"r": b}
	if m := worktreesByKey(sa); m["AAA-1"] == "" {
		t.Fatalf("a = %v", m)
	}
	if m := worktreesByKey(sb); m["BBB-2"] == "" || m["AAA-1"] != "" {
		t.Errorf("b got a's cache: %v", m)
	}
}
