package ui

import (
	"os/exec"
	"regexp"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Fix: checkout crashes on iOS 17!":                           "fix-checkout-crashes-on-ios-17",
		"Productpagina toont géén prijs":                             "productpagina-toont-g-n-prijs",
		"Add a very long summary that keeps going well past the cut": "add-a-very-long-summary-that-keeps-going",
		"!!!": "work",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIssueBranch(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	git("branch", "issue/ABC-12-other")
	if got := issueBranch(repo, "ABC-1"); got != "" {
		t.Errorf("ABC-1 matched %q", got)
	}
	git("branch", "issue/ABC-1-the-fix")
	if got := issueBranch(repo, "ABC-1"); got != "issue/ABC-1-the-fix" {
		t.Errorf("ABC-1 = %q", got)
	}
}

func TestJiraAgentName(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	for _, k := range []string{"JB-5598", "BTPWA-123456", "LONGPROJECTKEY-1234567"} {
		if n := jiraAgentName(k, time.Unix(1790000000, 0)); !valid.MatchString(n) {
			t.Errorf("jiraAgentName(%q) = %q", k, n)
		}
	}
}
