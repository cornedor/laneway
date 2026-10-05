package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/standup"
)

// Drafts: a comment or description being written is kept in the state
// file a moment after each change and on quit, so a crash or a quit
// doesn't lose it. Reopening the composer or editor on the issue brings it
// back; posting, saving or discarding drops it.

const (
	draftPrefix = jiraMetaPrefix + "draft:"
	// draftDelay is how long after a change the draft is written.
	draftDelay = 2 * time.Second
)

// commentDraft is the id of key's comment draft.
func commentDraft(key string) string { return "comment:" + key }

// descDraft is the id of the description editor's draft: of the issue's
// description, one of its comments or a multi-line field.
func (d *descEdit) draftID() string {
	id := "desc:" + d.key
	switch {
	case d.comment != "":
		id += ":comment:" + d.comment
	case d.field != "":
		id += ":field:" + d.field
	}
	return id
}

// saveDraft keeps text as id, dropping it when blank.
func (m *Model) saveDraft(id, text string) {
	if m.store == nil {
		return
	}
	if strings.TrimSpace(text) == "" {
		_ = m.store.DeleteMeta(draftPrefix + id)
		return
	}
	_ = m.store.SetMeta(draftPrefix+id, strconv.FormatInt(time.Now().Unix(), 10)+"\n"+text)
}

// draft is id's kept text and when it was written; false for none.
func (m *Model) draft(id string) (string, time.Time, bool) {
	if m.store == nil {
		return "", time.Time{}, false
	}
	v, ok, _ := m.store.GetMeta(draftPrefix + id)
	unix, text, _ := strings.Cut(v, "\n")
	sec, err := strconv.ParseInt(unix, 10, 64)
	if !ok || err != nil || text == "" {
		return "", time.Time{}, false
	}
	return text, time.Unix(sec, 0), true
}

func (m *Model) dropDraft(id string) {
	if m.store != nil {
		_ = m.store.DeleteMeta(draftPrefix + id)
	}
}

// saveOpenDrafts keeps what the open composer and editor hold, when it
// differs from where they started.
func (m *Model) saveOpenDrafts() {
	if m.jiraCommentActive && m.jiraCommentReplyTo == "" && m.jiraCommentInput.Value() != m.jiraCommentBefore {
		m.saveDraft(commentDraft(m.jiraCommentKey), m.jiraCommentInput.Value())
	}
	if d := m.descEdit; d != nil && d.input.Value() != d.before {
		m.saveDraft(d.draftID(), d.input.Value())
	}
}

type draftSaveMsg struct{}

// scheduleDraftSave writes the drafts draftDelay from now, once for any
// number of changes meanwhile.
func (m *Model) scheduleDraftSave() tea.Cmd {
	if m.draftPending {
		return nil
	}
	m.draftPending = true
	return tea.Tick(draftDelay, func(time.Time) tea.Msg { return draftSaveMsg{} })
}

func (m Model) handleDraftSave() (tea.Model, tea.Cmd) {
	m.draftPending = false
	m.saveOpenDrafts()
	return m, nil
}

// draftWhen is "15:04" today, "Mon 2 Jan 15:04" before.
func draftWhen(t, now time.Time) string {
	if standup.Day(t, now) == "Today" {
		return t.Local().Format("15:04")
	}
	return t.Local().Format("Mon 2 Jan 15:04")
}
