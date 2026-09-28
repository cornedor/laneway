package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestAskLLM: ctrl+a lists the questions; a pick pipes the issue to ui.llm
// with the question last, and the answer opens in the comment composer.
func TestAskLLM(t *testing.T) {
	var gotCmd []string
	var gotPrompt, gotIn string
	orig := runLLM
	t.Cleanup(func() { runLLM = orig })
	runLLM = func(_ context.Context, command []string, prompt, input string) (string, error) {
		gotCmd, gotPrompt, gotIn = command, prompt, input
		return "- [ ] logs in\n", nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.opts.llm = []string{"llm", "-m", "local"}
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Summary: "Login", Description: "Users log in.",
		Comments: []jira.Comment{{Author: "Ada", Body: "SSO too?"}}}
	m.openAsk()
	if len(m.jiraPicker.items) != len(asks) || m.jiraPicker.kind != jiraPickAsk {
		t.Fatalf("picker %v", m.jiraPicker.items)
	}
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	msg := cmd().(llmDoneMsg)
	if strings.Join(gotCmd, " ") != "llm -m local" || !strings.Contains(gotPrompt, "acceptance criteria") {
		t.Errorf("ran %v %q", gotCmd, gotPrompt)
	}
	for _, want := range []string{"# ABC-1 Login", "Users log in.", "### Ada", "SSO too?"} {
		if !strings.Contains(gotIn, want) {
			t.Errorf("stdin lacks %q:\n%s", want, gotIn)
		}
	}
	out, _ = m.handleLLMDone(msg)
	m = out.(Model)
	if !m.jiraCommentActive || m.jiraCommentInput.Value() != "- [ ] logs in" || !strings.Contains(m.status, "ctrl+s posts") {
		t.Errorf("composer %v %q, status %q", m.jiraCommentActive, m.jiraCommentInput.Value(), m.status)
	}

	m.jiraCommentActive = false
	m.jiraIssue = &jira.Issue{Key: "ABC-2"}
	out, _ = m.handleLLMDone(msg)
	if m = out.(Model); m.jiraCommentActive {
		t.Error("an answer about another issue opened the composer")
	}
}
