package ui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// TestGitCommits: your commits since the day, keyed by their subject;
// others' and older ones left out.
func TestGitCommits(t *testing.T) {
	repo := t.TempDir()
	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(cmd.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skip("git:", err, string(out))
		}
	}
	git(nil, "init", "-q")
	git(nil, "config", "user.email", "me@x.test")
	git(nil, "config", "user.name", "Me")
	old := []string{"GIT_AUTHOR_DATE=2020-01-01T10:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T10:00:00Z"}
	git(old, "commit", "-q", "--allow-empty", "-m", "ABC-1 long ago")
	git(nil, "commit", "-q", "--allow-empty", "-m", "Fix login ABC-2")
	git(nil, "commit", "-q", "--allow-empty", "-m", "utf-8 names in paths")
	git([]string{"GIT_AUTHOR_EMAIL=bob@x.test"}, "commit", "-q", "--allow-empty", "-m", "ABC-3 bob's")
	got := gitCommits([]string{repo, repo, t.TempDir()}, time.Now().Add(-time.Hour))
	var rows []string
	for _, e := range got {
		rows = append(rows, e.Key+"|"+e.What)
	}
	if strings.Join(rows, "\n") != "|commit: utf-8 names in paths\nABC-2|commit: Fix login ABC-2" {
		t.Errorf("commits:\n%s", strings.Join(rows, "\n"))
	}
}

// TestStandupCommits: a commit joins its issue's line in the text, keyless
// ones a no-ticket line after the issues.
func TestStandupCommits(t *testing.T) {
	now := time.Now()
	entries := withCommits(
		[]jira.InboxEntry{{Key: "ABC-2", Summary: "Login", When: now.Add(-2 * time.Minute), What: "status: To Do → Done"}},
		[]jira.InboxEntry{{When: now.Add(-3 * time.Minute), What: "commit: tidy"}, {Key: "ABC-2", When: now.Add(-time.Minute), What: "commit: fix"}})
	if want := "Today\n- ABC-2 Login: status: To Do → Done; commit: fix\n- no ticket: commit: tidy"; standupText(entries) != want {
		t.Errorf("text:\n%s\nwant:\n%s", standupText(entries), want)
	}
}
