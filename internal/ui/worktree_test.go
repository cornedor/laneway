package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestWorktreeRemove: a done issue's merged, clean worktree is removed
// through herdr; uncommitted changes or unmerged work keep it.
func TestWorktreeRemove(t *testing.T) {
	repo := t.TempDir()
	wt := filepath.Join(t.TempDir(), "abc-1")
	git := func(dir string, args ...string) {
		t.Helper()
		args = append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "init")
	git(repo, "worktree", "add", "-q", "-b", "issue/ABC-1-fix", wt)

	if wts := linkedWorktrees(repo); len(wts) != 1 {
		t.Fatalf("linked worktrees %v", wts)
	}
	path, branch := issueWorktree(repo, defaultWorkBranch, "ABC-1", "Bug")
	if branch != "issue/ABC-1-fix" || filepath.Base(path) != "abc-1" {
		t.Fatalf("worktree %q %q", path, branch)
	}
	if p, _ := issueWorktree(repo, defaultWorkBranch, "ABC-2", "Bug"); p != "" {
		t.Errorf("ABC-2 matched %q", p)
	}
	if why := worktreeGone(repo, path, branch, "main"); why != "" {
		t.Errorf("merged and clean should go: %s", why)
	}
	os.WriteFile(filepath.Join(wt, "wip"), []byte("x"), 0o644)
	if why := worktreeGone(repo, path, branch, "main"); !strings.Contains(why, "uncommitted") {
		t.Errorf("dirty: %q", why)
	}
	git(wt, "add", "wip")
	git(wt, "commit", "-q", "-m", "wip")
	if why := worktreeGone(repo, path, branch, "main"); !strings.Contains(why, "not merged") {
		t.Errorf("unmerged: %q", why)
	}
	git(repo, "merge", "-q", "--ff-only", branch)

	m := jiraTabModel(t)
	c, calls := fakeHerdrCalls(t)
	m.herdr = c
	m.jiraRepos = map[string]string{"ABC": repo}
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Type: "Bug", StatusCategory: "done"}
	m.openIssueActions()
	if !strings.Contains(pickerIDs(m.jiraPicker.items), "worktree-remove") {
		t.Fatalf("done issue should offer removal: %s", pickerIDs(m.jiraPicker.items))
	}
	done := m.applyIssueAction("ABC-1", "worktree-remove")().(agentDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	if got := <-calls; !strings.HasPrefix(got, "worktree.open") || !strings.Contains(got, `"branch":"issue/ABC-1-fix"`) {
		t.Errorf("open: %q", got)
	}
	if got := <-calls; !strings.HasPrefix(got, "worktree.remove") || !strings.Contains(got, `"force":false`) {
		t.Errorf("remove: %q", got)
	}
}

func pickerIDs(items []jiraPickerItem) string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.id)
	}
	return strings.Join(ids, " ")
}
