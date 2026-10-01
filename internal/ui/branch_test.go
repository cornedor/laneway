package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

func TestBranchKey(t *testing.T) {
	for in, want := range map[string]string{
		"issue/ABC-12-small-fix": "ABC-12",
		"abc-7":                  "ABC-7",
		"feature/LAN2-3":         "LAN2-3",
		"main":                   "",
		"":                       "",
	} {
		if got := work.BranchKey(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

// TestBranchIssue: the branch's issue opens, shows as a header chip that
// opens it again, and leads the palette.
func TestBranchIssue(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleBranchIssue(branchIssueMsg{"ABC-2"})
	m = out.(Model)
	if !m.refOpen || m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Fatal("the branch's issue should open")
	}
	if !strings.Contains(ansi.Strip(joinSegs(m.jiraTitleSegs())), "⎇ ABC-2") {
		t.Error("header lacks the branch chip")
	}
	m.closeRef()
	out, _, ok := m.runSeg(headSeg{kind: "branch"})
	if m = out.(Model); !ok || !m.refOpen {
		t.Error("the chip should open the issue")
	}
	m.closeRef()
	m = typePalette(t, m, "")
	if got := paletteLabels(m); len(got) == 0 || got[0] != "branch  ABC-2" {
		t.Errorf("palette starts %q", got[:min(len(got), 2)])
	}
}

// TestDetectBranchIssue: a key Jira knows is the branch's issue; one it
// doesn't (fix/utf-8) is none.
func TestDetectBranchIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/ABC-12") {
			io.WriteString(w, `{"key":"ABC-12","fields":{"summary":"x"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	old := gitBranch
	t.Cleanup(func() { gitBranch = old })
	for branch, want := range map[string]tea.Msg{"issue/ABC-12-fix": branchIssueMsg{"ABC-12"}, "fix/utf-8": nil, "main": nil} {
		gitBranch = func() string { return branch }
		if got := m.detectBranchIssue()(); got != want {
			t.Errorf("%s: %v, want %v", branch, got, want)
		}
	}
}
