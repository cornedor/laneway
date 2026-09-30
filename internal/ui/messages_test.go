package ui

import (
	"errors"
	"testing"
	"time"
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

// TestNoticesClearThemselves: a notice leaves the status line on its own,
// an error and a confirm prompt stay.
func TestNoticesClearThemselves(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.Update(keyMsg(t, "y"))
	m = out.(Model)
	later := m.statusAt.Add(noticeFor + time.Second)
	if m.shownStatus(m.statusAt) != "copied ABC-1" || m.shownStatus(later) != "" {
		t.Errorf("notice: %q, then %q", m.shownStatus(m.statusAt), m.shownStatus(later))
	}
	m.fail("jira: down")
	if m.shownStatus(later) != "jira: down" {
		t.Error("an error cleared itself")
	}
	m.status = "C again completes Sprint 1"
	if m.shownStatus(later) == "" {
		t.Error("a confirm prompt cleared itself")
	}
}
