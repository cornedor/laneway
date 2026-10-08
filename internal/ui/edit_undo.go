package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// u takes back the latest change, not only a card move: a field set in the
// panel, a quick or bulk edit, a sprint move or a deleted comment. The
// write that undoes it is built when the change is made, from the values
// before it.

// undoStep is a change to take back: what it was, and the way back.
type undoStep struct {
	what string
	back func(m *Model) tea.Cmd
}

// undoMax is how many changes u can take back.
const undoMax = 50

// pushUndo keeps back as the way back from the change just made; nothing
// while an undo runs, so taking a change back is not a change to take back.
func (m *Model) pushUndo(what string, back func(m *Model) tea.Cmd) {
	t := m.jiraTab
	if t.undoing {
		return
	}
	t.undo = append(t.undo, undoStep{what: what, back: back})
	if n := len(t.undo); n > undoMax {
		t.undo = slices.Delete(t.undo, 0, n-undoMax)
	}
}

// joinUndo makes the last two steps one, the latest's name: a drop that
// moved a card and changed its band.
func (t *jiraTabState) joinUndo() {
	n := len(t.undo)
	if n < 2 {
		return
	}
	a, b := t.undo[n-2], t.undo[n-1]
	t.undo = append(t.undo[:n-2], undoStep{what: b.what, back: func(m *Model) tea.Cmd { return tea.Batch(a.back(m), b.back(m)) }})
}

// undoMore is " · 2 more to undo", "" when nothing is left.
func (t *jiraTabState) undoMore() string {
	if len(t.undo) == 0 {
		return ""
	}
	return fmt.Sprintf(i18n.T(" · %d more to undo"), len(t.undo))
}

// recordUndo keeps run as the way back from the change just made.
func (m *Model) recordUndo(what string, run func(ctx context.Context) error) {
	m.pushUndo(what, func(m *Model) tea.Cmd {
		m.status = i18n.Tf("undoing %s…", what)
		ctx := m.ctx
		return func() tea.Msg { return editUndoneMsg{what: what, err: run(ctx)} }
	})
}

type editUndoneMsg struct {
	what string
	err  error
}

func (m Model) handleEditUndone(msg editUndoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(i18n.Tf("undo %s: %s", msg.what, msg.err.Error()))
		return m, nil
	}
	m.status = i18n.Tf("undid %s", msg.what) + m.jiraTab.undoMore()
	cmds := []tea.Cmd{m.refreshJiraAfterEdit()}
	if m.refOpen {
		cmds = append(cmds, m.loadCurrentRef())
	}
	return m, tea.Batch(cmds...)
}

// cardOf is key's card on the board, else as it was when marked.
func (m *Model) cardOf(key string) (jira.Card, bool) {
	for _, c := range m.jiraTab.cards {
		if c.Key == key {
			return c, true
		}
	}
	c := m.jiraTab.marked[key]
	return c, c.Key == key
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
		m.recordUndo(i18n.Tf("%s priority", key), func(ctx context.Context) error { return c.SetPriority(ctx, key, prev) })
	case jiraPickAssignee:
		prev := iss.AssigneeAccountID
		m.recordUndo(i18n.Tf("%s assignee", key), func(ctx context.Context) error { return c.SetAssignee(ctx, key, prev) })
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
	var name string
	switch field {
	case "summary":
		name = i18n.T("summary")
		prev := iss.Summary
		run = func(ctx context.Context) error { return c.SetSummary(ctx, key, prev) }
	case "labels":
		name = i18n.T("labels")
		prev := iss.Labels
		run = func(ctx context.Context) error { return c.SetLabels(ctx, key, prev) }
	case "points":
		name = i18n.T("points")
		prev := iss.StoryPoints
		run = func(ctx context.Context) error { return c.SetStoryPoints(ctx, key, prev) }
	default:
		return
	}
	m.recordUndo(i18n.Tf("%s %s", key, name), run)
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
	label := i18n.Tf("%s on %s", what, keys[0])
	if len(keys) > 1 {
		label = fmt.Sprintf(i18n.T("%s on %d issues"), what, len(keys))
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
		m.undoEach(i18n.T("priority"), keys, func(ctx context.Context, cd jira.Card) error {
			ps, err := c.Priorities(ctx)
			if err != nil {
				return err
			}
			for _, p := range ps {
				if p.Name == cd.Priority {
					return c.SetPriority(ctx, cd.Key, p.ID)
				}
			}
			return fmt.Errorf(i18n.T("no priority %q"), cd.Priority)
		})
	case jiraPickAssignee:
		m.undoEach(i18n.T("assignee"), keys, func(ctx context.Context, cd jira.Card) error { return c.SetAssignee(ctx, cd.Key, cd.AssigneeID) })
	case jiraPickStatus:
		m.undoEach(i18n.T("status"), keys, func(ctx context.Context, cd jira.Card) error {
			ts, err := c.Transitions(ctx, cd.Key)
			if err != nil {
				return err
			}
			for _, t := range ts {
				if t.StatusID == cd.StatusID || strings.EqualFold(t.Name, cd.Status) {
					return c.DoTransition(ctx, cd.Key, t.ID)
				}
			}
			return fmt.Errorf(i18n.T("no move back to %s"), cd.Status)
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
		m.recordUndo(i18n.Tf("the sprint move of %s", strings.Join(keys, ", ")), func(ctx context.Context) error { return c.MoveToSprint(ctx, sprint, keys...) })
	case v.kind == jiraViewBacklog:
		m.recordUndo(i18n.Tf("the sprint move of %s", strings.Join(keys, ", ")), func(ctx context.Context) error { return c.MoveToBacklog(ctx, keys...) })
	}
}

// undoDeleteComment records the way back from deleting cm on key: posting
// its body again, as you, under the comment it replied to.
func (m *Model) undoDeleteComment(key string, cm jira.Comment) {
	c, raw := m.jiraClient, json.RawMessage(cm.Raw)
	if !m.opts.threaded {
		cm.ParentID = ""
	}
	m.recordUndo(i18n.Tf("the deleted comment on %s", key), func(ctx context.Context) error {
		return c.AddCommentADFFor(ctx, key, raw, jira.Visibility{}, cm.ParentID)
	})
}
