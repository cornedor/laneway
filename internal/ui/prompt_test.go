package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// TestReadPrompt: the branch's issue with its cached status, the timer and
// the inbox count, all from the state file.
func TestReadPrompt(t *testing.T) {
	old := headBranch
	t.Cleanup(func() { headBranch = old })
	headBranch = func() string { return "issue/ABC-2-fix" }
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p := ReadPrompt(st, time.Now()); p.String() != "ABC-2" {
		t.Errorf("empty state = %q, want only the key", p)
	}
	raw, _ := json.Marshal(jiraCache{Cards: []jira.Card{{Key: "ABC-1", Status: "New"}, {Key: "ABC-2", Status: "In review"}}})
	now := time.Unix(1_800_000_000, 0)
	_ = st.SetMeta(jiraCacheKey(1, "Sprint 1"), string(raw))
	_ = st.SetMeta(timerMeta, "ABC-2 "+strconv.FormatInt(now.Add(-80*time.Minute).Unix(), 10))
	_ = st.SetMeta(inboxUnreadMeta, "3")
	if got := ReadPrompt(st, now).String(); got != "ABC-2 · In review · ⏱ 1h 20m · ✉ 3" {
		t.Errorf("prompt = %q", got)
	}
	headBranch = func() string { return "main" }
	if got := ReadPrompt(st, now).String(); got != "⏱ ABC-2 1h 20m · ✉ 3" {
		t.Errorf("off a branch = %q, want the timer naming its issue", got)
	}
}

// TestHeadBranch reads the branch from .git/HEAD, a worktree's through its
// .git file.
func TestHeadBranch(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "w"), 0o700)
	_ = os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/issue/ABC-7-x\n"), 0o600)
	_ = os.WriteFile(filepath.Join(repo, ".git", "worktrees", "w", "HEAD"), []byte("ref: refs/heads/ABC-8\n"), 0o600)
	sub := filepath.Join(repo, "a", "b")
	wt := filepath.Join(t.TempDir(), "w")
	_ = os.MkdirAll(sub, 0o700)
	_ = os.MkdirAll(wt, 0o700)
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git", "worktrees", "w")+"\n"), 0o600)
	for dir, want := range map[string]string{sub: "issue/ABC-7-x", wt: "ABC-8"} {
		t.Chdir(dir)
		if got := headBranch(); got != want {
			t.Errorf("in %s: %q, want %q", dir, got, want)
		}
	}
}
