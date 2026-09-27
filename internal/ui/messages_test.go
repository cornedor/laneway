package ui

import (
	"errors"
	"testing"
)

// TestSearchFailuresSay: a failed issue search, mention search or JQL
// completion says so instead of looking like no matches.
func TestSearchFailuresSay(t *testing.T) {
	boom := errors.New("offline")
	m := jiraTabModel(t)
	m.openPalette()
	out, _ := m.handlePaletteFound(paletteFoundMsg{seq: m.jiraPicker.fetchSeq, err: boom})
	if m = out.(Model); m.statusErr != "issue search: offline" {
		t.Errorf("palette: %q", m.statusErr)
	}
	m.closeJiraPicker()
	m.openJQL()
	out, _ = m.handleJQLValues(jqlValuesMsg{seq: m.jql.seq, err: boom})
	if m = out.(Model); m.statusErr != "completions: offline" {
		t.Errorf("jql: %q", m.statusErr)
	}
	m.jql = nil
	m.jiraCommentActive = true
	out, _ = m.handleMentionFound(mentionFoundMsg{seq: m.jiraMention.seq, err: boom})
	if m = out.(Model); m.statusErr != "mention search: offline" {
		t.Errorf("mention: %q", m.statusErr)
	}
}
