package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// u takes back the latest change, not only a card move: a field set in the
// panel, a quick or bulk edit, a sprint move or a deleted comment. The
// write that undoes it is built when the change is made, from the values
// before it.

// editUndo is a change to take back: what it was, and the write that puts
// things back. seq orders it among the moves and band drops (undoSeq).
type editUndo struct {
	what string
	seq  int
	run  func(ctx context.Context) error
}

// recordUndo keeps run as the way back from the change just made.
func (m *Model) recordUndo(what string, run func(ctx context.Context) error) {
	t := m.jiraTab
	t.undoSeq++
	t.lastEdit = &editUndo{what: what, seq: t.undoSeq, run: run}
}

// lastEditIsLatest is whether the last edit came after the last move and
// band drop.
func (t *jiraTabState) lastEditIsLatest() bool {
	return t.lastEdit != nil && t.lastEdit.seq > t.moveSeq && t.lastEdit.seq > t.lastBand.seq
}

type editUndoneMsg struct {
	what string
	err  error
}

// undoEdit runs the last edit's way back, once.
func (m *Model) undoEdit() tea.Cmd {
	e := m.jiraTab.lastEdit
	m.jiraTab.lastEdit = nil
	m.status = "undoing " + e.what + "…"
	ctx := m.ctx
	return func() tea.Msg { return editUndoneMsg{what: e.what, err: e.run(ctx)} }
}

func (m Model) handleEditUndone(msg editUndoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail("undo " + msg.what + ": " + msg.err.Error())
		return m, nil
	}
	m.status = "undid " + msg.what
	cmds := []tea.Cmd{m.refreshJiraAfterEdit()}
	if m.refOpen {
		cmds = append(cmds, m.loadCurrentRef())
	}
	return m, tea.Batch(cmds...)
}

// cardOf is key's card on the board.
func (m *Model) cardOf(key string) (jira.Card, bool) {
	for _, c := range m.jiraTab.cards {
		if c.Key == key {
			return c, true
		}
	}
	return jira.Card{}, false
}

// undoPanelPick records the way back from the panel's priority or
// assignee pick on key.
func (m *Model) undoPanelPick(kind jiraPickerKind, key string) {
	iss, c := m.jiraIssue, m.jiraClient
	if iss == nil || iss.Key != key {
		return
	}
	switch kind {
	case jiraPickPriority:
		prev := iss.PriorityID
		m.recordUndo(key+" priority", func(ctx context.Context) error { return c.SetPriority(ctx, key, prev) })
	case jiraPickAssignee:
		prev := iss.AssigneeAccountID
		m.recordUndo(key+" assignee", func(ctx context.Context) error { return c.SetAssignee(ctx, key, prev) })
	}
}

// undoPanelField records the way back from the panel's summary, labels or
// points input on key.
func (m *Model) undoPanelField(field, key string) {
	iss, c := m.jiraIssue, m.jiraClient
	if iss == nil || iss.Key != key {
		return
	}
	var run func(ctx context.Context) error
	switch field {
	case "summary":
		prev := iss.Summary
		run = func(ctx context.Context) error { return c.SetSummary(ctx, key, prev) }
	case "labels":
		prev := iss.Labels
		run = func(ctx context.Context) error { return c.SetLabels(ctx, key, prev) }
	case "points":
		prev := iss.StoryPoints
		run = func(ctx context.Context) error { return c.SetStoryPoints(ctx, key, prev) }
	default:
		return
	}
	m.recordUndo(key+" "+field, run)
}

// undoEach records the way back from a change to keys, each put back by
// back from its card as it was.
func (m *Model) undoEach(what string, keys []string, back func(ctx context.Context, c jira.Card) error) {
	var cards []jira.Card
	for _, k := range keys {
		if cd, ok := m.cardOf(k); ok {
			cards = append(cards, cd)
		}
	}
	if len(cards) == 0 {
		return
	}
	label := what + " on " + keys[0]
	if len(keys) > 1 {
		label = fmt.Sprintf("%s on %d issues", what, len(keys))
	}
	m.recordUndo(label, func(ctx context.Context) error {
		var errs []string
		for _, cd := range cards {
			if err := back(ctx, cd); err != nil {
				errs = append(errs, cd.Key+": "+err.Error())
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return nil
	})
}

// undoBulkPick records the way back from a bulk or quick priority,
// assignee or status pick.
func (m *Model) undoBulkPick(kind jiraPickerKind, keys []string) {
	c := m.jiraClient
	switch kind {
	case jiraPickPriority:
		m.undoEach("priority", keys, func(ctx context.Context, cd jira.Card) error {
			ps, err := c.Priorities(ctx)
			if err != nil {
				return err
			}
			for _, p := range ps {
				if p.Name == cd.Priority {
					return c.SetPriority(ctx, cd.Key, p.ID)
				}
			}
			return fmt.Errorf("no priority %q", cd.Priority)
		})
	case jiraPickAssignee:
		m.undoEach("assignee", keys, func(ctx context.Context, cd jira.Card) error { return c.SetAssignee(ctx, cd.Key, cd.AssigneeID) })
	case jiraPickStatus:
		m.undoEach("status", keys, func(ctx context.Context, cd jira.Card) error {
			ts, err := c.Transitions(ctx, cd.Key)
			if err != nil {
				return err
			}
			for _, t := range ts {
				if t.StatusID == cd.StatusID || strings.EqualFold(t.Name, cd.Status) {
					return c.DoTransition(ctx, cd.Key, t.ID)
				}
			}
			return fmt.Errorf("no move back to %s", cd.Status)
		})
	}
}

// undoSprintMove records the way back from moving keys to a sprint or the
// backlog: to the sprint or backlog the board shows them in.
func (m *Model) undoSprintMove(keys []string) {
	v, ok := m.jiraCurrentView()
	c := m.jiraClient
	switch {
	case !ok:
	case v.kind == jiraViewSprint:
		sprint := v.sprint
		m.recordUndo("the sprint move of "+strings.Join(keys, ", "), func(ctx context.Context) error { return c.MoveToSprint(ctx, sprint, keys...) })
	case v.kind == jiraViewBacklog:
		m.recordUndo("the sprint move of "+strings.Join(keys, ", "), func(ctx context.Context) error { return c.MoveToBacklog(ctx, keys...) })
	}
}

// undoDeleteComment records the way back from deleting cm on key: posting
// its body again, as you.
func (m *Model) undoDeleteComment(key string, cm jira.Comment) {
	c, raw := m.jiraClient, json.RawMessage(cm.Raw)
	m.recordUndo("the deleted comment on "+key, func(ctx context.Context) error { return c.AddCommentADF(ctx, key, raw) })
}
