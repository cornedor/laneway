package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// Refinement: ctrl+e on the board steps through the view's issues one at a
// time, the unestimated first, each in a wide panel where its keys set
// points, priority, labels, status and more (A splits it into subtasks).
// J goes on, K back; esc ends it and copies what changed as a list.

type refineState struct {
	keys    []string
	idx     int
	changes []string // "ABC-1 points updated", as the status line said
	pct     int      // the panel width before, put back after
}

// refineWidth is the panel's share while refining.
const refineWidth = 80

// startRefine queues the view's cards, unestimated first, and opens the
// first.
func (m *Model) startRefine() tea.Cmd {
	cards := slices.Clone(m.jiraTab.cards)
	if len(cards) == 0 {
		m.status = "no issues in this view to refine"
		return nil
	}
	slices.SortStableFunc(cards, func(a, b jira.Card) int {
		switch {
		case a.Points == "" && b.Points != "":
			return -1
		case a.Points != "" && b.Points == "":
			return 1
		}
		return 0
	})
	keys := make([]string, len(cards))
	for i, c := range cards {
		keys[i] = c.Key
	}
	m.refine = &refineState{keys: keys, pct: m.opts.panelPct}
	m.opts.panelPct = max(m.opts.panelPct, refineWidth)
	return m.refineShow()
}

// refineShow opens the queue's current issue in the panel.
func (m *Model) refineShow() tea.Cmd {
	r := m.refine
	m.selectJiraKey(r.keys[r.idx])
	out, cmd := m.openJiraKey(r.keys[r.idx])
	*m = out.(Model)
	m.refBack = nil // stepping isn't a trail to walk back
	m.focus = focusRef
	m.resize()
	m.status = fmt.Sprintf("refining %d of %d · J next · K back · esc done", r.idx+1, len(r.keys))
	return cmd
}

// refineKey handles J, K and esc while refining; false for other keys.
func (m *Model) refineKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	r := m.refine
	if r == nil {
		return nil, false
	}
	switch msg.String() {
	case "J":
		if r.idx == len(r.keys)-1 {
			m.status = "that was the last · esc ends refining"
			return nil, true
		}
		r.idx++
		return m.refineShow(), true
	case "K":
		if r.idx == 0 {
			m.status = "this is the first"
			return nil, true
		}
		r.idx--
		return m.refineShow(), true
	case "esc":
		if m.panelFieldSel() != "" {
			return nil, false // the field cursor goes first
		}
		return m.endRefine(), true
	}
	return nil, false
}

// endRefine closes the panel, puts its width back and copies the changes.
func (m *Model) endRefine() tea.Cmd {
	r := m.refine
	m.refine = nil
	m.opts.panelPct = r.pct
	m.closeRef()
	if len(r.changes) == 0 {
		m.status = fmt.Sprintf("refined %d issues, nothing changed", r.idx+1)
		return nil
	}
	m.status = fmt.Sprintf("refined %d of %d issues, %s · copied as a list", r.idx+1, len(r.keys), plural(len(r.changes), "change"))
	return tea.SetClipboard("- " + strings.Join(r.changes, "\n- "))
}

// noteRefine keeps a write that went through while refining.
func (m *Model) noteRefine(status string) {
	if m.refine != nil && status != "" {
		m.refine.changes = append(m.refine.changes, status)
	}
}
