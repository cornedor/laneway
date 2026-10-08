package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
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

// Draft is a kept draft: its text, when it was written and, for one that
// edits a document, the jira.DocBase of the document it was written on,
// for the save to check against Jira.
type Draft struct {
	Text, Base string
	At         time.Time
}

// EncodeDraft is how a draft is kept: "unix base\ntext", or "unix\ntext"
// without a base.
func EncodeDraft(d Draft) string {
	head := strconv.FormatInt(d.At.Unix(), 10)
	if d.Base != "" {
		head += " " + d.Base
	}
	return head + "\n" + d.Text
}

// DecodeDraft reads a kept draft; false for none.
func DecodeDraft(v string) (Draft, bool) {
	head, text, _ := strings.Cut(v, "\n")
	unix, base, _ := strings.Cut(head, " ")
	sec, err := strconv.ParseInt(unix, 10, 64)
	if err != nil || text == "" {
		return Draft{}, false
	}
	return Draft{Text: text, Base: base, At: time.Unix(sec, 0)}, true
}

// saveDraft keeps text as id, written on base; blank drops it.
func (m *Model) saveDraft(id, text, base string) {
	if m.store == nil {
		return
	}
	if strings.TrimSpace(text) == "" {
		_ = m.store.DeleteMeta(draftPrefix + id)
		return
	}
	_ = m.store.SetMeta(draftPrefix+id, EncodeDraft(Draft{Text: text, Base: base, At: time.Now()}))
}

// draft is id's kept draft; false for none.
func (m *Model) draft(id string) (Draft, bool) {
	if m.store == nil {
		return Draft{}, false
	}
	v, ok, _ := m.store.GetMeta(draftPrefix + id)
	if !ok {
		return Draft{}, false
	}
	return DecodeDraft(v)
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
		m.saveDraft(commentDraft(m.jiraCommentKey), m.jiraCommentInput.Value(), "")
	}
	if d := m.descEdit; d != nil && d.input.Value() != d.before {
		m.saveDraft(d.draftID(), d.input.Value(), d.base)
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
	if standup.Day(t, now) == i18n.T("Today") {
		return t.Local().Format("15:04")
	}
	return t.Local().Format("Mon 2 Jan 15:04")
}
