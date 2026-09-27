package ui

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestOpenPullRequest: A's row pushes the issue's branch and opens a draft
// with gh for a GitHub origin, glab else.
func TestOpenPullRequest(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "x"}, {"branch", "issue/ABC-1-fix-login"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git:", string(out))
		}
	}
	old := runIn
	t.Cleanup(func() { runIn = old })
	var ran []string
	remote := "git@github.com:me/abc.git"
	runIn = func(_ context.Context, dir, name string, args ...string) (string, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		switch {
		case name == "git" && args[0] == "remote":
			return remote, nil
		case name == "gh" || name == "glab":
			return "Creating draft…\nhttps://example.test/pr/7", nil
		}
		return "", nil
	}
	m := jiraTabModel(t)
	m.jiraRepos = map[string]string{"ABC": repo}
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Summary: "Fix login", Type: "Bug"}
	m.openIssueActions()
	if !slices.ContainsFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == "pr" }) {
		t.Fatal("no pull request row")
	}
	m.closeJiraPicker()
	cmd := m.applyIssueAction("ABC-1", "pr")
	if cmd == nil {
		t.Fatalf("no command: %q", m.status)
	}
	out, _ := m.Update(cmd())
	m = out.(Model)
	if !strings.Contains(m.status, "https://example.test/pr/7") {
		t.Errorf("status %q", m.status)
	}
	want := []string{"git remote get-url origin", "git push -u origin issue/ABC-1-fix-login",
		"gh pr create --draft --head issue/ABC-1-fix-login --title ABC-1 Fix login --body Jira: https://example.atlassian.net/browse/ABC-1"}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Errorf("ran:\n%s", strings.Join(ran, "\n"))
	}
	ran, remote = nil, "git@gitlab.example:me/abc.git"
	m.applyIssueAction("ABC-1", "pr")()
	if !strings.HasPrefix(ran[2], "glab mr create --draft --source-branch issue/ABC-1-fix-login") {
		t.Errorf("gitlab: %q", ran[2])
	}
	m.jiraIssue = &jira.Issue{Key: "ABC-2", Summary: "No branch"}
	if cmd := m.applyIssueAction("ABC-2", "pr"); cmd != nil || !strings.Contains(m.status, "no branch for ABC-2") {
		t.Errorf("no branch: %q", m.status)
	}
}
