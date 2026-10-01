package ui

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/work"
)

// The issue of the git branch laneway starts in: opened on start, a ⎇ chip
// in the header and first in the palette.

// gitBranch is the working directory's git branch, "" outside a repo.
var gitBranch = func() string {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// branchIssueMsg is the branch's issue found in Jira.
type branchIssueMsg struct{ key string }

// detectBranchIssue looks for the branch's issue, which must exist: a
// branch like fix/utf-8 names none.
func (m *Model) detectBranchIssue() tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	if !c.Enabled() {
		return nil
	}
	return func() tea.Msg {
		key := work.BranchKey(gitBranch())
		if key == "" {
			return nil
		}
		if _, err := c.Get(ctx, key); err != nil {
			return nil
		}
		return branchIssueMsg{key}
	}
}

// handleBranchIssue opens the branch's issue, selected on the board when
// it's there.
func (m Model) handleBranchIssue(msg branchIssueMsg) (tea.Model, tea.Cmd) {
	m.branchKey = msg.key
	return m.openBranchIssue()
}

// openBranchIssue shows the branch's issue in the panel.
func (m Model) openBranchIssue() (tea.Model, tea.Cmd) {
	m.selectJiraKey(m.branchKey)
	m.renderJira()
	return m.openJiraKey(m.branchKey)
}
