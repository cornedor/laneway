package ui

import (
	"bufio"
	"encoding/json"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/herdr"
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
	if got := issueBranch(repo, defaultWorkBranch, "ABC-1", "Bug"); got != "" {
		t.Errorf("ABC-1 matched %q", got)
	}
	git("branch", "issue/ABC-1-the-fix")
	if got := issueBranch(repo, defaultWorkBranch, "ABC-1", "Bug"); got != "issue/ABC-1-the-fix" {
		t.Errorf("ABC-1 = %q", got)
	}
	git("branch", "bug/ABC-1")
	if got := issueBranch(repo, "{type}/{key}-{summary}", "ABC-1", "Bug"); got != "bug/ABC-1" {
		t.Errorf("typed template = %q", got)
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

func TestBranchPattern(t *testing.T) {
	for _, tc := range []struct {
		tmpl, name string
		want       bool
	}{
		{defaultWorkBranch, "issue/ABC-1-fix-it", true},
		{defaultWorkBranch, "issue/ABC-1", true},
		{defaultWorkBranch, "issue/ABC-12-fix", false},
		{defaultWorkBranch, "feature/ABC-1-fix", false},
		{"{key}-{summary}", "ABC-1-x", true},
		{"{key}-{summary}", "issue/ABC-1-x", false},
		{"{project}/{type}/{key}", "ABC/story/ABC-1", true},
		{"{project}/{type}/{key}", "ABC/bug/ABC-1", false},
	} {
		if got := branchPattern(tc.tmpl, "ABC-1", "Story").MatchString(tc.name); got != tc.want {
			t.Errorf("%q ~ %q = %v", tc.tmpl, tc.name, got)
		}
	}
	o, _ := optionsFrom(config.UIConfig{BranchTemplate: "{type}/{key}"})
	if o.workBranch != "{type}/{key}" {
		t.Errorf("work branch follows branch_template: %q", o.workBranch)
	}
	o, _ = optionsFrom(config.UIConfig{BranchTemplate: "{type}/{key}", WorkBranchTemplate: "wip/{key}"})
	if o.workBranch != "wip/{key}" || o.branchTemplate != "{type}/{key}" {
		t.Errorf("work branch template = %q, copy = %q", o.workBranch, o.branchTemplate)
	}
}

// TestWorkAgent: start work launches ui.work_agent's kind in the worktree.
func TestWorkAgent(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	kinds := make(chan string, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var req struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			_ = json.Unmarshal(line, &req)
			switch req.Method {
			case "worktree.open":
				conn.Write([]byte(`{"id":"x","result":{"root_pane":{"pane_id":"w1:p1","tab_id":"w1:t1"},"workspace":{"workspace_id":"w1"},"worktree":{"path":"/wt/x"}}}` + "\n"))
			case "agent.start":
				kinds <- req.Params["kind"].(string)
				fallthrough
			default:
				conn.Write([]byte(`{"id":"x","result":{}}` + "\n"))
			}
			conn.Close()
		}
	}()
	msg := jiraWork(herdr.New(sock), t.TempDir(), defaultWorkBranch, "codex", "ABC-1", "Bug", "Fix", "go")().(jiraWorkMsg)
	if msg.err != nil || msg.agent != "codex" || <-kinds != "codex" {
		t.Fatalf("msg = %+v", msg)
	}
	m := jiraTabModel(t)
	out, _ := m.handleJiraWork(msg)
	if s := out.(Model).status; s != "ABC-1: codex started in /wt/x" {
		t.Errorf("status = %q", s)
	}
}
