package ui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// Bulk edit: x marks cards (across views), B edits every marked one at once
// with the panel's editors — status, priority, assignee, labels, points,
// sprint. Writes run a few at a time; failures are reported by key and keep
// their marks.

// bulkDoneMsg is a bulk edit answered: what changed and which keys failed.
type bulkDoneMsg struct {
	what   string
	keys   []string
	failed map[string]error
}

// toggleJiraMark marks or unmarks the selected card and steps down.
func (m *Model) toggleJiraMark() {
	t := m.jiraTab
	c, ok := m.selectedJiraCard()
	if !ok {
		return
	}
	if t.marked == nil {
		t.marked = map[string]bool{}
	}
	if t.marked[c.Key] {
		delete(t.marked, c.Key)
	} else {
		t.marked[c.Key] = true
	}
	t.rows = nil
	m.moveJiraCursor(1)
	m.status = fmt.Sprintf("%d marked · %s edits them · esc clears", len(t.marked), helpKey(m.keys.Bulk))
}

// toggleJiraMarkAll marks every card the cursor's lane shows (the list's
// rows in list mode, a search narrowing them), or unmarks them when all
// already are.
func (m *Model) toggleJiraMarkAll() {
	t := m.jiraTab
	idx := t.order
	if m.jiraShowsLanes() {
		if t.lane >= len(t.lanes) {
			return
		}
		idx = t.lanes[t.lane].cards
	}
	if len(idx) == 0 {
		return
	}
	if t.marked == nil {
		t.marked = map[string]bool{}
	}
	all := !slices.ContainsFunc(idx, func(i int) bool { return !t.marked[t.cards[i].Key] })
	for _, i := range idx {
		if all {
			delete(t.marked, t.cards[i].Key)
		} else {
			t.marked[t.cards[i].Key] = true
		}
	}
	t.rows = nil
	m.renderJira()
	m.status = fmt.Sprintf("%d marked · %s edits them · esc clears", len(t.marked), helpKey(m.keys.Bulk))
}

// markedKeys are the marked cards, sorted.
func (m *Model) markedKeys() []string {
	keys := make([]string, 0, len(m.jiraTab.marked))
	for k := range m.jiraTab.marked {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (m *Model) clearJiraMarks() {
	m.jiraTab.marked = nil
	m.jiraTab.rows = nil
	m.renderJira()
}

// jiraMark is the marked card's sign, "" when not marked.
func (m *Model) jiraMark(key string) string {
	if !m.jiraTab.marked[key] {
		return ""
	}
	return lipgloss.NewStyle().Foreground(focusedColor).Bold(true).Render("✓")
}

// openBulkMenu asks what to change on the marked cards.
func (m *Model) openBulkMenu() {
	keys := m.markedKeys()
	if len(keys) == 0 {
		m.status = "mark cards with " + helpKey(m.keys.Mark) + " first"
		return
	}
	m.startJiraPicker(jiraPickBulk, fmt.Sprintf("Edit %d marked", len(keys)), false)
	m.setJiraPickerItems([]jiraPickerItem{
		{id: "status", label: "Status"},
		{id: "priority", label: "Priority"},
		{id: "assignee", label: "Assignee"},
		{id: "labels", label: "Labels (+add -remove)"},
		{id: "points", label: "Story points"},
		{id: "sprint", label: "Sprint / backlog"},
		{id: "clear", label: "Clear marks"},
	})
}

// applyBulkMenu opens the picked field's editor for the marked cards.
func (m *Model) applyBulkMenu(id string) tea.Cmd {
	keys := m.markedKeys()
	title := func(what string) string { return fmt.Sprintf("%s — %d issues", what, len(keys)) }
	client, ctx := m.jiraClient, m.ctx
	switch id {
	case "status":
		// Moves are per issue; the first one's names stand for all.
		gen := m.startJiraPicker(jiraPickStatus, title("Set status"), false)
		seq := m.jiraPicker.fetchSeq
		m.jiraPicker.bulk = keys
		return func() tea.Msg {
			opts, err := client.Transitions(ctx, keys[0])
			items := make([]jiraPickerItem, len(opts))
			for i, o := range opts {
				items[i] = jiraPickerItem{id: o.Name, label: o.Name}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStatus, items: items, err: err}
		}
	case "priority":
		gen := m.startJiraPicker(jiraPickPriority, title("Set priority"), false)
		seq := m.jiraPicker.fetchSeq
		m.jiraPicker.bulk = keys
		return func() tea.Msg {
			opts, err := client.Priorities(ctx)
			items := make([]jiraPickerItem, len(opts))
			for i, o := range opts {
				items[i] = jiraPickerItem{id: o.ID, label: o.Name}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickPriority, items: items, err: err}
		}
	case "assignee":
		gen := m.startJiraPicker(jiraPickAssignee, title("Set assignee"), true)
		m.jiraPicker.issueKey = keys[0]
		m.jiraPicker.curAssignee = "-" // no ✓: the marked cards differ
		m.jiraPicker.bulk = keys
		return m.fetchAssignees(gen, m.jiraPicker.fetchSeq, keys[0], "")
	case "labels":
		m.openBulkInput("bulk-labels", "ui -old: add ui, remove old")
	case "points":
		m.openBulkInput("bulk-points", "number (empty clears)")
	case "sprint":
		m.openJiraSprintPicker()
		if m.jiraPicker.active {
			m.jiraPicker.title = title("Move")
			m.jiraPicker.bulk = keys
		}
	case "clear":
		m.clearJiraMarks()
	}
	return nil
}

// openBulkInput opens the field input for a text edit of the marked cards.
func (m *Model) openBulkInput(field, placeholder string) {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = placeholder
	ti.SetWidth(max(min(m.width-16, 60), 16))
	ti.Focus()
	m.jiraFieldInput = ti
	m.jiraFieldActive = true
	m.jiraFieldName = field
	m.jiraFieldKey = fmt.Sprintf("%d issues", len(m.jiraTab.marked))
}

// applyBulkPick writes a picked status, priority, assignee or sprint to
// every key.
func (m *Model) applyBulkPick(kind jiraPickerKind, keys []string, it jiraPickerItem) tea.Cmd {
	client := m.jiraClient
	switch kind {
	case jiraPickStatus:
		return m.prepareBulkMove(keys, it.id)
	case jiraPickPriority:
		return m.runBulk("priority "+it.label, keys, func(ctx context.Context, key string) error {
			return client.SetPriority(ctx, key, it.id)
		})
	case jiraPickAssignee:
		return m.runBulk("assignee", keys, func(ctx context.Context, key string) error {
			return client.SetAssignee(ctx, key, it.id)
		})
	case jiraPickSprint:
		it.current = false // the marked cards may sit anywhere
		return m.moveJiraToSprint(keys, it)
	}
	return nil
}

// bulkMoveMsg is a bulk status change worked out on its first card: go
// (form nil), or ask the form's fields once for all.
type bulkMoveMsg struct {
	keys []string
	to   string
	form *jiraFormState
	err  error
}

// prepareBulkMove checks, on the first marked card, what the move to status
// to needs; the fields asked there go with the move on every card.
func (m *Model) prepareBulkMove(keys []string, to string) tea.Cmd {
	want := func(t jira.TransitionMeta) bool { return strings.EqualFold(t.ToName, to) }
	c, ctx := m.jiraClient, m.ctx
	m.status = "checking what " + to + " needs…"
	return func() tea.Msg {
		// Only look: prepareJiraMove would move a card that needs nothing.
		metas, err := c.TransitionsMeta(ctx, keys[0])
		if err != nil {
			return bulkMoveMsg{keys: keys, to: to, err: err}
		}
		i := slices.IndexFunc(metas, want)
		if i < 0 || !metas[i].HasScreen {
			return bulkMoveMsg{keys: keys, to: to}
		}
		ic, err := c.IssueContext(ctx, keys[0])
		if err != nil {
			return bulkMoveMsg{keys: keys, to: to, err: err}
		}
		rules, _ := c.TransitionRules(ctx, ic.Project, ic.TypeID)
		if len(rules[metas[i].ID].Required) == 0 {
			return bulkMoveMsg{keys: keys, to: to}
		}
		form := buildJiraForm(keys[0], metas[i], rules[metas[i].ID], ic)
		form.bulk = keys
		return bulkMoveMsg{keys: keys, to: to, form: form}
	}
}

// handleBulkMove moves the cards, or opens the form once for all of them.
func (m Model) handleBulkMove(msg bulkMoveMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.status = "status: " + msg.err.Error()
		return m, nil
	case msg.form != nil:
		m.jiraForm = msg.form
		m.status = fmt.Sprintf("→ %s needs a few fields, for all %d cards", msg.to, len(msg.keys))
		return m, nil
	}
	return m, m.bulkTransition(msg.keys, msg.to, nil, "")
}

// bulkTransition moves every key to status to, along the transition each
// issue's workflow offers, with fields and comment when given.
func (m *Model) bulkTransition(keys []string, to string, fields map[string]any, comment string) tea.Cmd {
	c := m.jiraClient
	return m.runBulk("status "+to, keys, func(ctx context.Context, key string) error {
		metas, err := c.TransitionsMeta(ctx, key)
		if err != nil {
			return err
		}
		for _, t := range metas {
			if strings.EqualFold(t.ToName, to) {
				return c.TransitionWith(ctx, key, t.ID, fields, comment)
			}
		}
		return fmt.Errorf("no move to %s", to)
	})
}

// applyBulkField writes the bulk input's labels or points.
func (m Model) applyBulkField(field, raw string) (tea.Model, tea.Cmd) {
	keys := m.markedKeys()
	client := m.jiraClient
	m.closeJiraField()
	if field == "bulk-points" {
		return m, m.runBulk("points", keys, func(ctx context.Context, key string) error {
			return client.SetStoryPoints(ctx, key, raw)
		})
	}
	var add, remove []string
	for _, w := range strings.Fields(raw) {
		if l, ok := strings.CutPrefix(w, "-"); ok {
			remove = append(remove, l)
		} else {
			add = append(add, strings.TrimPrefix(w, "+"))
		}
	}
	if len(add)+len(remove) == 0 {
		return m, nil
	}
	return m, m.runBulk("labels", keys, func(ctx context.Context, key string) error {
		return client.EditLabels(ctx, key, add, remove)
	})
}

// bulkWorkers is how many writes run at once: quick, without tripping Jira's
// rate limit.
const bulkWorkers = 4

// runBulk runs write for every key, a few at a time.
func (m *Model) runBulk(what string, keys []string, write func(ctx context.Context, key string) error) tea.Cmd {
	m.status = fmt.Sprintf("updating %s on %d issues…", what, len(keys))
	ctx := m.ctx
	return func() tea.Msg {
		failed := map[string]error{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, bulkWorkers)
		for _, k := range keys {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				if err := write(ctx, k); err != nil {
					mu.Lock()
					failed[k] = err
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return bulkDoneMsg{what: what, keys: keys, failed: failed}
	}
}

// handleBulkDone reports the edit, keeps only the failed cards marked and
// refetches the board (and the panel's issue when it was one of them).
func (m Model) handleBulkDone(msg bulkDoneMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	t.marked = nil
	for k := range msg.failed {
		if t.marked == nil {
			t.marked = map[string]bool{}
		}
		t.marked[k] = true
	}
	t.rows = nil
	ok := len(msg.keys) - len(msg.failed)
	m.status = fmt.Sprintf("%s set on %d", msg.what, ok)
	if len(msg.failed) > 0 {
		var fails []string
		for _, k := range slices.Sorted(maps.Keys(msg.failed)) {
			fails = append(fails, k+": "+msg.failed[k].Error())
		}
		m.status += fmt.Sprintf(" · %d failed (still marked): %s", len(fails), strings.Join(fails, "; "))
	}
	cmds := []tea.Cmd{m.refreshJiraAfterEdit()}
	if r := m.currentRef(); r != nil && slices.Contains(msg.keys, r.jiraKey) {
		cmds = append(cmds, m.loadCurrentRef())
	}
	m.renderJira()
	return m, tea.Batch(cmds...)
}
