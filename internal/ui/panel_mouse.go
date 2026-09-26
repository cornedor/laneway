package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Clicks in the issue panel: a field is selected by one click and edited by
// a second (or a double-click); a linked issue opens.

// panelHit is what a panel line does when clicked: field >= 0 selects that
// field, else key opens that issue.
type panelHit struct {
	field int
	key   string
}

// indexPanelHits finds the clickable lines of the panel's content: the
// fields where the render wrote them, the links under their heading.
func (m *Model) indexPanelHits(content string) {
	m.panelHits = map[int]panelHit{}
	for i, l := range m.panelFieldLine {
		m.panelHits[l] = panelHit{field: i}
	}
	iss := m.jiraIssue
	if iss == nil || len(iss.Links) == 0 {
		return
	}
	head := fmt.Sprintf("Links (%d)  L open", len(iss.Links))
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if ansi.Strip(l) != head {
			continue
		}
		for j, lk := range iss.Links {
			if i+1+j < len(lines) {
				m.panelHits[i+1+j] = panelHit{field: -1, key: lk.Key}
			}
		}
		return
	}
}

// panelLineAt is the content line on screen row y of the panel, -1 above
// its body. The body soft-wraps, so rows count wrapped lines.
func (m *Model) panelLineAt(y int) int {
	top := 1 + min(len(m.refBack), refCrumbsShown+1) // the title, then the trail
	row := y - top
	if row < 0 || row >= m.refView.Height() {
		return -1
	}
	v := row + m.refView.YOffset()
	lines := strings.Split(m.refView.GetContent(), "\n")
	w := m.refView.Width()
	seen := 0
	for i, l := range lines {
		n := visualRowsBefore([]string{l}, 1, w)
		if v < seen+n {
			return i
		}
		seen += n
	}
	return -1
}

// clickPanel acts on a clicked panel line.
func (m Model) clickPanel(h panelHit, count int) (tea.Model, tea.Cmd) {
	if h.field < 0 {
		return m.openJiraKey(h.key)
	}
	if m.panelFieldIdx() == h.field || count == 2 {
		m.fieldCursor, m.fieldCursorKey = h.field, m.jiraIssue.Key
		return m, m.editPanelField()
	}
	m.fieldCursor, m.fieldCursorKey = h.field, m.jiraIssue.Key
	m.renderRef()
	return m, nil
}
