package vcs_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/vcs"
	"github.com/cornedor/laneway/internal/vcs/vcstest"
)

// A jj repo's branch is the nearest bookmark that isn't trunk's, its
// workspaces its checkouts, and laneway makes, checks and drops them.
func TestJJWorkspaces(t *testing.T) {
	repo, origin, jj := vcstest.JJRepo(t)
	ctx := context.Background()
	if !vcs.IsJJ(repo) || !vcs.IsRepo(repo) {
		t.Fatal("not a jj repo")
	}
	if got := vcs.DefaultBase(repo); got != "main@origin" {
		t.Errorf("base = %q", got)
	}
	if got := vcs.Branch(repo); got != "" {
		t.Errorf("on main, branch = %q, want none", got)
	}
	if url, err := vcs.RemoteURL(ctx, repo); err != nil || url != origin {
		t.Errorf("remote = %q %v", url, err)
	}

	branch := "issue/ABC-1-fix"
	path, err := vcs.AddWorkspace(ctx, repo, branch, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "repo.issue-ABC-1-fix" || filepath.Dir(path) != filepath.Dir(repo) {
		t.Errorf("workspace at %s", path)
	}
	if got := vcs.Checkouts(repo); len(got) != 1 || got[path] != branch {
		t.Errorf("checkouts = %v", got)
	}
	if got := vcs.Branch(path); got != branch {
		t.Errorf("workspace branch = %q", got)
	}
	if !vcs.HasBranch(repo, branch) || !slices.Contains(vcs.Branches(repo), branch) || vcs.HasBranch(repo, "nope") {
		t.Errorf("branches = %v", vcs.Branches(repo))
	}
	if err := vcs.Kept(repo, path, branch, ""); err != nil {
		t.Errorf("fresh workspace kept: %v", err)
	}

	if err := os.WriteFile(filepath.Join(path, "b"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vcs.Kept(repo, path, branch, ""); !errors.Is(err, vcs.ErrUncommitted) {
		t.Errorf("edited workspace: %v", err)
	}
	jj(path, "commit", "-m", "ABC-1 fix it")
	if err := vcs.Kept(repo, path, branch, ""); !errors.Is(err, vcs.ErrUnmerged) {
		t.Errorf("committed, not merged: %v", err)
	}

	// Past the bookmark, the change is still the branch's.
	from := time.Now().Add(-time.Hour)
	commits, ok := vcs.Commits(repo, from)
	if !ok || !slices.ContainsFunc(commits, func(c vcs.Commit) bool { return c.Subject == "ABC-1 fix it" }) {
		t.Errorf("commits = %v %v", commits, ok)
	}
	snaps := vcs.Snapshots(repo, from)
	if !slices.ContainsFunc(snaps, func(s vcs.Snapshot) bool { return s.Subject == "ABC-1 fix it" && s.Bookmark == branch }) {
		t.Errorf("snapshots = %+v", snaps)
	}
	changes, err := vcs.BranchChanges(ctx, path)
	if err != nil || len(changes) != 1 || changes[0].Description != "ABC-1 fix it\n" {
		t.Fatalf("branch changes = %+v %v", changes, err)
	}
	if err := vcs.Describe(ctx, path, changes[0].ID, "ABC-1 fix it well"); err != nil {
		t.Fatal(err)
	}

	// Merged: main moves onto the change and is pushed.
	jj(repo, "bookmark", "set", "main", "-r", "description(substring:\"fix it well\")")
	if _, err := vcs.Push(ctx, repo, "main"); err != nil {
		t.Fatal(err)
	}
	if err := vcs.Fetch(ctx, repo, "main"); err != nil {
		t.Error("fetch:", err)
	}
	if err := vcs.Kept(repo, path, branch, ""); err != nil {
		t.Errorf("merged: %v", err)
	}

	if err := vcs.ForgetWorkspace(ctx, repo, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("directory stays: %v", err)
	}
	if got := vcs.Checkouts(repo); len(got) != 0 {
		t.Errorf("checkouts after forget = %v", got)
	}
}

// Outside jj, git answers as before.
func TestGitRepo(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "ABC-1 init"}, {"branch", "issue/ABC-2-x"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git:", err, string(out))
		}
	}
	if vcs.IsJJ(repo) || !vcs.IsRepo(repo) {
		t.Fatal("not a git repo")
	}
	if got := vcs.Branch(repo); got != "main" {
		t.Errorf("branch = %q", got)
	}
	if got := vcs.Branches(repo); !slices.Equal(got, []string{"issue/ABC-2-x", "main"}) {
		t.Errorf("branches = %v", got)
	}
	if got := vcs.DefaultBase(repo); got != "" {
		t.Errorf("base without a remote = %q", got)
	}
	if vcs.Snapshots(repo, time.Time{}) != nil {
		t.Error("git has no snapshots")
	}
}

// A merged branch's bookmark is history, not the branch you're on: work
// on trunk names none. A branch whose bookmark is still at the commit it
// left trunk from, while trunk moved on, is still its own.
func TestJJBranchPastMerged(t *testing.T) {
	repo, _, jj := vcstest.JJRepo(t)
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// issue/ABC-1-old merged into main.
	jj(repo, "new", "main", "-m", "ABC-1 old")
	write("old")
	jj(repo, "bookmark", "create", "issue/ABC-1-old", "-r", "@")
	jj(repo, "new", "-m", "merge")
	write("merge")
	jj(repo, "bookmark", "set", "main", "-r", "@")
	if _, err := vcs.Push(context.Background(), repo, "main"); err != nil {
		t.Fatal(err)
	}
	// A change on trunk without a bookmark of its own, the local main
	// behind main@origin as when you don't pull it along.
	jj(repo, "bookmark", "set", "main", "--allow-backwards", "-r", "description(exact:\"init\n\")")
	jj(repo, "new", "main@origin", "-m", "ABC-2 new")
	write("new")
	if got := vcs.Branch(repo); got != "" {
		t.Errorf("on trunk past a merged branch: %q", got)
	}
	// A branch started from main, its bookmark not moved; main moves on.
	jj(repo, "bookmark", "set", "main", "-r", "main@origin")
	jj(repo, "bookmark", "create", "issue/ABC-3-lag", "-r", "main")
	jj(repo, "new", "main", "-m", "ABC-3 work")
	write("lag")
	branchTip := jj(repo, "log", "--no-graph", "-r", "@", "-T", "change_id")
	jj(repo, "new", "main", "-m", "more on main")
	write("more")
	jj(repo, "bookmark", "set", "main", "-r", "@")
	if _, err := vcs.Push(context.Background(), repo, "main"); err != nil {
		t.Fatal(err)
	}
	jj(repo, "edit", branchTip)
	if got := vcs.Branch(repo); got != "issue/ABC-3-lag" {
		t.Errorf("lagging bookmark: %q", got)
	}
}
