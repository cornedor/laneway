package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

func TestKeyedMessage(t *testing.T) {
	for _, c := range []struct{ msg, branch, want string }{
		{"fix it\n", "ABC-12", "ABC-12 fix it\n"},
		{"# comment\n\nfix it\n", "ABC-12", "# comment\n\nABC-12 fix it\n"},
		{"fix it LAN-3\n", "ABC-12", "fix it LAN-3\n"},
		{"Merge branch 'x'\n", "", "Merge branch 'x'\n"},
		{"fixup! fix it\n", "", "fixup! fix it\n"},
		{"# only comments\n", "", "# only comments\n"},
		{"fix it\n# ------------------------ >8 ------------------------\n+ABC-9 in the diff\n", "ABC-12",
			"ABC-12 fix it\n# ------------------------ >8 ------------------------\n+ABC-9 in the diff\n"},
	} {
		got, err := keyedMessage(c.msg, c.branch)
		if err != nil || got != c.want {
			t.Errorf("%q on %q = %q %v, want %q", c.msg, c.branch, got, err, c.want)
		}
	}
	if _, err := keyedMessage("fix it\n", ""); err == nil {
		t.Error("no key anywhere should refuse the commit")
	}
}

// TestHookInstall writes both hooks, keeps someone else's, and replaces it
// with -force.
func TestHookInstall(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Skip("no git:", string(out))
	}
	t.Chdir(repo)
	other := filepath.Join(repo, ".git", "hooks", "post-checkout")
	_ = os.MkdirAll(filepath.Dir(other), 0o755)
	_ = os.WriteFile(other, []byte("#!/bin/sh\necho mine\n"), 0o755)
	var out, errOut bytes.Buffer
	if code := subcommand([]string{"hook", "install", "-strict"}, "", "club", &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "someone else's hook") {
		t.Errorf("exit %d, %q", code, errOut.String())
	}
	b, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "commit-msg"))
	if !strings.Contains(string(b), `exec laneway hook commit-msg -site 'club' -strict "$1"`) {
		t.Errorf("commit-msg hook:\n%s", b)
	}
	if b, _ := os.ReadFile(other); string(b) != "#!/bin/sh\necho mine\n" {
		t.Error("someone else's hook was replaced")
	}
	errOut.Reset()
	if code := subcommand([]string{"hook", "install", "-force"}, "", "", &out, &errOut); code != 0 {
		t.Errorf("-force: exit %d, %q", code, errOut.String())
	}
	if b, _ := os.ReadFile(other); !strings.Contains(string(b), `exec laneway hook post-checkout "$@"`) {
		t.Errorf("post-checkout hook:\n%s", b)
	}
}

// rw is a terminal: what is typed, and what was written to it.
type rw struct {
	io.Reader
	bytes.Buffer
}

func (r *rw) Read(p []byte) (int, error) { return r.Reader.Read(p) }

// TestOfferStart asks to move a to-do issue in progress and moves it on y.
func TestOfferStart(t *testing.T) {
	var moved string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/issue/ABC-12/transitions" && r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			moved = string(b)
		case r.URL.Path == "/rest/api/3/issue/ABC-12/transitions":
			io.WriteString(w, `{"transitions":[{"id":"31","to":{"name":"Done","statusCategory":{"key":"done"}}},
				{"id":"21","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
		case r.URL.Path == "/rest/api/3/issue/ABC-12":
			io.WriteString(w, `{"key":"ABC-12","fields":{"summary":"x","status":{"name":"To Do","statusCategory":{"key":"new"}}}}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	c := jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	tty := &rw{Reader: strings.NewReader("y\n")}
	offerStart(context.Background(), c, "ABC-12", tty, io.Discard)
	if !strings.Contains(tty.String(), "move ABC-12 (To Do) to In Progress? [y/N]") || !strings.Contains(moved, `"21"`) {
		t.Errorf("asked %q, moved %q", tty.String(), moved)
	}
	moved = ""
	offerStart(context.Background(), c, "ABC-12", &rw{Reader: strings.NewReader("\n")}, io.Discard)
	if moved != "" {
		t.Error("enter alone should leave it")
	}
}
