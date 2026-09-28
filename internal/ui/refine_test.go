package ui

import (
	"strings"
	"testing"
)

// TestRefine: ctrl+e queues the view's issues, unestimated first; J and K
// step; esc ends, puts the panel back and lists the changes.
func TestRefine(t *testing.T) {
	m := jiraTabModel(t)
	pct := m.opts.panelPct
	out, _ := m.handleJiraKey(keyMsg(t, "ctrl+e"))
	m = out.(Model)
	r := m.refine
	if r == nil || strings.Join(r.keys, " ") != "ABC-1 ABC-2 ABC-4 ABC-3" {
		t.Fatalf("queue %v", r)
	}
	if !m.refOpen || m.refs[m.refIdx].jiraKey != "ABC-1" || m.opts.panelPct != refineWidth || !strings.Contains(m.status, "refining 1 of 4") {
		t.Fatalf("first: open %v, pct %d, status %q", m.refOpen, m.opts.panelPct, m.status)
	}
	out, _ = m.handleRefKey(keyStr("J"))
	m = out.(Model)
	if m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Errorf("J: %q", m.refs[m.refIdx].jiraKey)
	}
	out, _ = m.handleJiraMutated(jiraMutatedMsg{key: "ABC-2", field: "points"})
	m = out.(Model)
	out, _ = m.handleRefKey(keyStr("K"))
	m = out.(Model)
	if m.refs[m.refIdx].jiraKey != "ABC-1" {
		t.Errorf("K: %q", m.refs[m.refIdx].jiraKey)
	}
	out, cmd := m.handleRefKey(keyMsg(t, "esc"))
	m = out.(Model)
	if m.refine != nil || m.refOpen || m.opts.panelPct != pct || cmd == nil || !strings.Contains(m.status, "1 change ·") {
		t.Errorf("end: refine %v, open %v, pct %d, status %q", m.refine, m.refOpen, m.opts.panelPct, m.status)
	}
}

// TestRefineView: refining takes what the view shows, minus done cards.
func TestRefineView(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.cards[3].Done = true
	for _, k := range []string{"/", "o", "enter"} {
		out, _ := m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	out, _ := m.handleJiraKey(keyMsg(t, "ctrl+e"))
	m = out.(Model)
	if m.refine == nil || strings.Join(m.refine.keys, " ") != "ABC-2" {
		t.Fatalf("queue %v, want ABC-2 (Second; Fourth is done)", m.refine)
	}
}
