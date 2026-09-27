package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Several issues at once: a list pasted into the create form's summary,
// one summary a line (bullets and checkboxes dropped), becomes that many
// issues with the form's type, parent, sprint and fields. Typing in the
// summary drops the list.

// batchBullet is a list line's marker: "- [ ] ", "* ", "1. ", "• ".
var batchBullet = regexp.MustCompile(`^(?:[-*+•]\s+)?(?:\[[ xX]\]\s+)?(?:\d+[.)]\s+)?`)

// batchLines are the summaries in pasted text, "" lines left out.
func batchLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(batchBullet.ReplaceAllString(strings.TrimSpace(l), "")); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// pasteBatch takes a paste of two or more lines into the create form's
// summary as a list of issues; false for anything else.
func (m *Model) pasteBatch(text string) bool {
	f := m.jiraForm
	if f == nil || f.create == nil || !f.create.form || !f.editing || f.multiline || f.fields[f.idx].ID != createSummaryField {
		return false
	}
	lines := batchLines(text)
	if len(lines) < 2 {
		return false
	}
	f.create.batch = lines
	f.input.SetValue(lines[0])
	f.input.CursorEnd()
	f.message = fmt.Sprintf("%d issues from the pasted lines, each with this form's type and fields · ↵ creates them all · typing here drops the list", len(lines))
	return true
}

// dropBatch forgets a pasted list once the summary is typed in.
func (f *jiraFormState) dropBatch() {
	if f.create != nil && f.create.batch != nil {
		f.create.batch, f.message = nil, ""
	}
}

type batchCreatedMsg struct {
	keys []string
	left []string // the summaries not made, from the one that failed
	err  error
}

// createBatch makes one issue per summary of cr's batch, in order,
// stopping at the first refused.
func (m *Model) createBatch(cr jiraFormCreate) tea.Cmd {
	c, ctx, lines := m.jiraClient, m.ctx, cr.batch
	m.status = fmt.Sprintf("creating %d issues…", len(lines))
	return func() tea.Msg {
		var keys []string
		for i, s := range lines {
			one := cr
			one.in.Summary = s
			cctx, cancel := context.WithTimeout(ctx, c.Scaled(30*time.Second))
			msg := createIssue(cctx, c, one, "")
			cancel()
			if msg.key == "" {
				return batchCreatedMsg{keys: keys, left: lines[i:], err: msg.err}
			}
			keys = append(keys, msg.key)
		}
		return batchCreatedMsg{keys: keys}
	}
}

// handleBatchCreated closes the form when all were made; a refusal keeps
// it with the summaries left.
func (m Model) handleBatchCreated(msg batchCreatedMsg) (tea.Model, tea.Cmd) {
	made := fmt.Sprintf("created %d: %s", len(msg.keys), strings.Join(msg.keys, ", "))
	if f := m.jiraForm; msg.err != nil && f != nil && f.create != nil {
		f.busy, f.create.batch = false, msg.left
		f.err = fmt.Sprintf("%q: %v", msg.left[0], msg.err)
		f.message = fmt.Sprintf("%d issues left to create · ↵ tries again", len(msg.left))
		if len(msg.keys) > 0 {
			m.status = made
		}
		return m, m.refreshJiraAfterEdit()
	}
	m.jiraForm = nil
	m.status = made
	if msg.err != nil {
		m.fail(made + " · then " + msg.err.Error())
	}
	return m, m.refreshJiraAfterEdit()
}
