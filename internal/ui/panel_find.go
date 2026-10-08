package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/i18n"
)

// Find in the panel: / asks for text, the panel scrolls to its first line
// at or below the top, n and N step through the rest.

// openPanelFind asks what to find in the panel issue.
func (m *Model) openPanelFind() {
	if m.jiraIssue == nil {
		return
	}
	m.openBulkInput("find", i18n.Tf("text in %s", m.jiraIssue.Key))
	m.jiraFieldKey = m.jiraIssue.Key
	m.jiraFieldInput.SetValue(m.panelFind)
	m.jiraFieldInput.CursorEnd()
}

// applyPanelFind finds the typed text from the panel's top line down.
func (m *Model) applyPanelFind(raw string) {
	m.closeJiraField()
	m.panelFind = strings.TrimSpace(raw)
	if m.panelFind != "" {
		m.findInPanel(0)
	}
}

// findInPanel shows the next line holding panelFind after the last one
// shown (dir 1), the one before it (-1), or the first from the panel's top
// (0), wrapping round.
func (m *Model) findInPanel(dir int) {
	q := strings.ToLower(m.panelFind)
	var hits []int
	for i, l := range m.panelPlain {
		l = strings.ToLower(l)
		n := len(l)
		if i+1 < len(m.panelPlain) && i < len(m.panelSoft) && m.panelSoft[i] {
			// A wrapped row: a phrase may run on over the break.
			l += " " + strings.TrimLeft(strings.ToLower(m.panelPlain[i+1]), " ")
		}
		if at := strings.Index(l, q); at >= 0 && at < n {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		m.status = fmt.Sprintf("no %q in %s", m.panelFind, m.jiraFieldKeyOr())
		return
	}
	var n int
	switch dir {
	case 0:
		n = slices.IndexFunc(hits, func(h int) bool { return h >= m.refView.YOffset() })
	case 1:
		n = slices.IndexFunc(hits, func(h int) bool { return h > m.panelFindAt })
	default:
		n = len(hits) - 1
		for n >= 0 && hits[n] >= m.panelFindAt {
			n--
		}
	}
	if n < 0 {
		n = 0 // past the last: round to the first
		if dir < 0 {
			n = len(hits) - 1
		}
	}
	m.panelFindAt = hits[n]
	m.refView.SetYOffset(max(hits[n]-2, 0)) // two lines of context above
	m.status = fmt.Sprintf(i18n.T("%q %d of %d · n next · N previous"), m.panelFind, n+1, len(hits))
}

// jiraFieldKeyOr is the panel issue's key, for messages.
func (m *Model) jiraFieldKeyOr() string {
	if m.jiraIssue != nil {
		return m.jiraIssue.Key
	}
	return i18n.T("the issue")
}

// plainLines is content's lines without styling, for find.
func plainLines(content string) []string {
	return strings.Split(ansi.Strip(content), "\n")
}
