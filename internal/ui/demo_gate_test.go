package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// TestDemoRunsNothing: in the demo, opening a link, a configured action,
// asking the LLM, pasting and a screenshot each say so and run nothing.
func TestDemoRunsNothing(t *testing.T) {
	m := configuredJiraModel(t).WithDemo()
	m.jiraIssue = &jira.Issue{Key: "DEMO-5"}
	ran := filepath.Join(t.TempDir(), "ran")
	m.actions = []config.Action{{Name: "touch", Command: []string{"touch", ran}}}
	m.opts.llm = []string{"touch", ran}
	shot, clip := screenshot, clipboardImage
	t.Cleanup(func() { screenshot, clipboardImage = shot, clip })
	screenshot = func() ([]byte, error) { t.Error("screenshot taken"); return nil, nil }
	clipboardImage = func([]string) ([]byte, error) { t.Error("clipboard read"); return nil, nil }

	if msg, ok := m.openOpenable(openable{name: "DEMO-5", url: "https://example.com"})().(openedMsg); !ok || !errors.Is(msg.err, errOffInDemo) {
		t.Errorf("open = %+v", msg)
	}
	if cmd := m.runAction(0, true); cmd != nil {
		cmd()
	}
	m.openAsk()
	if !strings.Contains(m.status, "not in the demo") {
		t.Errorf("ask: status %q", m.status)
	}
	if cmd := m.askLLM("summary"); cmd != nil {
		cmd()
	}
	for _, act := range []string{"paste", "screenshot"} {
		if cmd := m.applyIssueAction("DEMO-5", act); cmd != nil {
			t.Errorf("%s: a command", act)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("a command ran")
	}
}
