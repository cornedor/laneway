package ui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// Bulk edit: x marks cards (across views), B edits every marked one at once
// with the panel's editors — status, priority, assignee, labels, points,
// sprint. Writes run a few at a time; failures are reported by key and keep
// their marks.

// bulkDoneMsg is a bulk edit answered: what changed and which keys failed.
// quick is a quick edit's (e), which leaves the marks alone.
type bulkDoneMsg struct {
	what   string
	keys   []string
	failed map[string]error
	quick  bool
}

// isMarked is whether key is marked.
func (t *jiraTabState) isMarked(key string) bool {
	_, ok := t.marked[key]
	return ok
}

// toggleMark marks c, keeping it as it is now, or unmarks it.
func (t *jiraTabState) toggleMark(c jira.Card) {
	if t.isMarked(c.Key) {
		delete(t.marked, c.Key)
		return
	}
	if t.marked == nil {
		t.marked = map[string]jira.Card{}
	}
	t.marked[c.Key] = c
}

// toggleJiraMark marks or unmarks the selected card and steps down.
func (m *Model) toggleJiraMark() {
	t := m.jiraTab
	c, ok := m.selectedJiraCard()
	if !ok {
		return
	}
	t.toggleMark(c)
	t.rows = nil
	m.moveJiraCursor(1)
	m.status = i18n.Tf("%d marked · %s edits them · esc clears", len(t.marked), helpKey(m.keys.Bulk))
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
		t.marked = map[string]jira.Card{}
	}
	all := !slices.ContainsFunc(idx, func(i int) bool { return !t.isMarked(t.cards[i].Key) })
	for _, i := range idx {
		if all {
			delete(t.marked, t.cards[i].Key)
		} else {
			t.marked[t.cards[i].Key] = t.cards[i]
		}
	}
	t.rows = nil
	m.renderJira()
	m.status = i18n.Tf("%d marked · %s edits them · esc clears", len(t.marked), helpKey(m.keys.Bulk))
}

// markedKeys are the marked cards, sorted; a quick edit's one card instead
// while it runs.
func (m *Model) markedKeys() []string {
	if m.quickKey != "" {
		return []string{m.quickKey}
	}
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
	if !m.jiraTab.isMarked(key) {
		return ""
	}
	return lipgloss.NewStyle().Foreground(focusedColor).Bold(true).Render("✓")
}

// bulkFields are what a bulk or quick edit changes.
func bulkFields() []jiraPickerItem {
	return []jiraPickerItem{
		{id: "status", label: i18n.T("Status")},
		{id: "priority", label: i18n.T("Priority")},
		{id: "assignee", label: i18n.T("Assignee")},
		{id: "labels", label: i18n.T("Labels (+add -remove)")},
		{id: "points", label: i18n.T("Story points")},
		{id: "sprint", label: i18n.T("Sprint / backlog")},
	}
}

// withValues shows each field's value on cards dim after its label: the
// shared one, "mixed" when they differ.
func withValues(items []jiraPickerItem, cards []jira.Card) []jiraPickerItem {
	if len(cards) == 0 {
		return items
	}
	value := func(id string, c jira.Card) string {
		switch id {
		case "status":
			return c.Status
		case "priority":
			return c.Priority
		case "assignee":
			return cmp.Or(c.Assignee, i18n.T("unassigned"))
		case "labels":
			return c.Labels
		case "points":
			return c.Points
		case "sprint":
			if c.Sprint == "" {
				return i18n.T("backlog")
			}
			return c.Sprint
		}
		return ""
	}
	w := 0
	for _, it := range items {
		w = max(w, visualWidth(it.label))
	}
	for i, it := range items {
		v := value(it.id, cards[0])
		for _, c := range cards[1:] {
			if value(it.id, c) != v {
				v = i18n.T("mixed")
			}
		}
		if v == "" {
			v = "—"
		}
		if it.id != "clear" && !strings.HasPrefix(it.id, "key:") {
			items[i].label = it.label + strings.Repeat(" ", w-visualWidth(it.label)+2) + jiraDimStyle.Render(v)
		}
	}
	return items
}

// cardsByKey are the cards with keys (cardOf), in the keys' order.
func (m *Model) cardsByKey(keys ...string) []jira.Card {
	var out []jira.Card
	for _, k := range keys {
		if c, ok := m.cardOf(k); ok {
			out = append(out, c)
		}
	}
	return out
}

// openBulkMenu asks what to change on the marked cards.
func (m *Model) openBulkMenu() {
	m.quickKey = ""
	keys := m.markedKeys()
	if len(keys) == 0 {
		m.status = i18n.Tf("mark cards with %s first", helpKey(m.keys.Mark))
		return
	}
	m.startJiraPicker(jiraPickBulk, i18n.Tf("Edit %d marked", len(keys)), false)
	m.setJiraPickerItems(append(withValues(bulkFields(), m.cardsByKey(keys...)), jiraPickerItem{id: "clear", label: i18n.T("Clear marks")}))
}

// openQuickEdit edits the selected card from the board with the bulk
// editors, without opening the panel.
func (m *Model) openQuickEdit() {
	if c, ok := m.selectedJiraCard(); ok {
		m.openQuickEditKey(c.Key)
	}
}

// openQuickEditKey edits key with the bulk editors.
func (m *Model) openQuickEditKey(key string) {
	m.quickKey = key
	m.startJiraPicker(jiraPickBulk, i18n.Tf("Edit %s", key), false)
	m.setJiraPickerItems(withValues(bulkFields(), m.cardsByKey(key)))
}

// submenuHint marks a card menu row that opens a list beside it.
const submenuHint = "▸"

// cardMenu is what a right-click on a card offers: the quick edit's fields,
// then the card's own keys, each shown with its key.
func (m *Model) cardMenu() []jiraPickerItem {
	items := bulkFields()
	for i, it := range items {
		switch it.id {
		case "status", "priority", "assignee", "sprint":
			items[i].hint = submenuHint
		case "labels":
			items[i].label = i18n.T("Labels")
		}
	}
	for _, a := range []struct {
		id, label string
		b         key.Binding
	}{
		{"open", i18n.T("Open in the panel"), m.keys.OpenChannel},
		{"browser", i18n.T("Open in the browser"), m.keys.OpenAttach},
		{"copy-key", i18n.T("Copy the key"), m.keys.CopyKey},
		{"pin", i18n.T("Pin / unpin"), m.keys.Pin},
	} {
		items = append(items, jiraPickerItem{id: "key:" + firstKey(a.b), label: a.label, hint: helpKey(a.b)})
	}
	return items
}

// cardClickable is whether x, y is on the board's cards, nothing over them.
func (m *Model) cardClickable(x, y int) bool {
	t := m.jiraTab
	listW, _ := m.jiraListWidth(m.width)
	menu := m.dropdown != nil && m.dropdownOn() // a right-click off it opens another
	if menu {
		if _, outside := m.pickerRowAt(x, y); !outside {
			return false
		}
		if d := m.dropdown; d.parent != "" && y >= d.top && y < d.top+lipgloss.Height(d.parent) && x >= d.left && x < d.left+lipgloss.Width(d.parent) {
			return false
		}
	}
	return y < m.bodyH() && (menu || !m.modalOpen() && !m.pickerOnTop() && !m.pickerInline()) && (!m.refOpen || x < listW) &&
		t.roadmap == nil && t.plan == nil && t.charts == nil && t.week == nil && t.standup == nil && t.inbox == nil && t.agentsView == nil && t.mrs == nil
}

// rightClickJira selects the card under h and opens its menu at x, y.
func (m Model) rightClickJira(h hit, x, y int) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if h.line < 0 {
		return m, nil
	}
	m.focus = focusJira
	if h.idx < 0 {
		t.idx = h.line
	} else {
		t.lane, t.row = h.idx, h.line
	}
	m.renderJira()
	m.closeJiraPicker() // a menu open on another card
	if c, ok := m.selectedJiraCard(); ok {
		m.openCardMenu(c.Key, point{x, y}, 0)
	}
	return m, nil
}

// applyBulkMenu opens the picked field's editor for the marked cards.
func (m *Model) applyBulkMenu(id string) tea.Cmd {
	keys := m.markedKeys()
	title := func(what string) string { return i18n.Tf("%s — %d issues", what, len(keys)) }
	client, ctx := m.jiraClient, m.ctx
	switch id {
	case "status":
		// Moves are per issue; the first one's names stand for all.
		gen := m.startJiraPicker(jiraPickStatus, title(i18n.T("Set status")), false)
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
		gen := m.startJiraPicker(jiraPickPriority, title(i18n.T("Set priority")), false)
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
		gen := m.startJiraPicker(jiraPickAssignee, title(i18n.T("Set assignee")), true)
		m.jiraPicker.issueKey = keys[0]
		m.jiraPicker.curAssignee = "-" // no ✓: the marked cards differ
		m.jiraPicker.bulk = keys
		return m.fetchAssignees(gen, m.jiraPicker.fetchSeq, keys[0], "")
	case "labels":
		m.openBulkInput("bulk-labels", i18n.T("ui -old: add ui, remove old"))
	case "points":
		m.openBulkInput("bulk-points", i18n.T("number (empty clears)"))
	case "sprint":
		m.openJiraSprintPicker()
		if m.jiraPicker.active {
			m.jiraPicker.title = title(i18n.T("Move"))
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
	m.jiraFieldKey = i18n.Tf("%d issues", len(m.markedKeys()))
	if keys := m.markedKeys(); len(keys) == 1 {
		m.jiraFieldKey = keys[0] // a quick edit, or one marked
	}
}

// applyBulkPick writes a picked status, priority, assignee or sprint to
// every key.
func (m *Model) applyBulkPick(kind jiraPickerKind, keys []string, it jiraPickerItem) tea.Cmd {
	client := m.jiraClient
	m.undoBulkPick(kind, keys)
	m.setRepeat(it.label, func(m *Model, key string) tea.Cmd {
		m.quickKey = key
		return m.applyBulkPick(kind, []string{key}, it)
	})
	switch kind {
	case jiraPickStatus:
		return m.prepareBulkMove(keys, it.id)
	case jiraPickPriority:
		return m.runBulk(i18n.Tf("priority %s", it.label), keys, func(ctx context.Context, key string) error {
			return client.SetPriority(ctx, key, it.id)
		})
	case jiraPickAssignee:
		return m.runBulk(i18n.T("assignee"), keys, func(ctx context.Context, key string) error {
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
	m.status = i18n.Tf("checking what %s needs…", to)
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
		rules, rErr := c.TransitionRules(ctx, ic.Project, ic.TypeID)
		// Unknown rules show the screen, as a single move does.
		if len(rules[metas[i].ID].Required) == 0 && (rErr == nil || len(metas[i].Fields) == 0) {
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
		m.fail(i18n.Tf("status: %s", msg.err.Error()))
		return m, nil
	case msg.form != nil:
		m.jiraForm = msg.form
		m.status = i18n.Tf("→ %s needs a few fields, for all %d cards", msg.to, len(msg.keys))
		return m, nil
	}
	return m, m.bulkTransition(msg.keys, msg.to, nil, "")
}

// bulkTransition moves every key to status to, along the transition each
// issue's workflow offers, with fields and comment when given.
func (m *Model) bulkTransition(keys []string, to string, fields map[string]any, comment string) tea.Cmd {
	c := m.jiraClient
	return m.runBulk(i18n.Tf("status %s", to), keys, func(ctx context.Context, key string) error {
		metas, err := c.TransitionsMeta(ctx, key)
		if err != nil {
			return err
		}
		for _, t := range metas {
			if strings.EqualFold(t.ToName, to) {
				return c.TransitionWith(ctx, key, t.ID, fields, comment)
			}
		}
		return errors.New(i18n.Tf("no move to %s", to))
	})
}

// bulkAsk asks to confirm writing label to n issues.
func bulkAsk(kind jiraPickerKind, label string, n int) string {
	switch kind {
	case jiraPickStatus, jiraPickSprint:
		return i18n.Tf("enter again moves %d issues to %s", n, label)
	case jiraPickPriority:
		return i18n.Tf("enter again sets priority %s on %d issues", label, n)
	case jiraPickAssignee:
		return i18n.Tf("enter again assigns %d issues to %s", n, label)
	}
	return i18n.Tf("enter again sets %s on %d issues", label, n)
}

// applyBulkField writes the bulk input's labels or points, on more than one
// issue once a second enter confirms it.
func (m Model) applyBulkField(field, raw string) (tea.Model, tea.Cmd) {
	keys := m.markedKeys()
	if len(keys) > 1 && m.jiraFieldAsked != raw {
		m.jiraFieldAsked = raw
		what := i18n.Tf("points %s", cmp.Or(strings.TrimSpace(raw), i18n.T("cleared")))
		if field == "bulk-labels" {
			what = i18n.Tf("labels %s", raw)
		}
		m.status = i18n.Tf("enter again sets %s on %d issues", what, len(keys))
		return m, nil
	}
	m.closeJiraField()
	m.setRepeat(strings.TrimPrefix(field, "bulk-")+" "+raw, func(m *Model, key string) tea.Cmd {
		m.quickKey = key
		return m.bulkFieldWrite(field, raw, []string{key})
	})
	return m, m.bulkFieldWrite(field, raw, keys)
}

// bulkFieldWrite writes labels (+add -remove) or points to keys.
func (m *Model) bulkFieldWrite(field, raw string, keys []string) tea.Cmd {
	client := m.jiraClient
	if field == "bulk-points" {
		m.undoEach("points", keys, func(ctx context.Context, cd jira.Card) error { return client.SetStoryPoints(ctx, cd.Key, cd.Points) })
		return m.runBulk(i18n.T("points"), keys, func(ctx context.Context, key string) error {
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
		return nil
	}
	m.undoEach("labels", keys, func(ctx context.Context, cd jira.Card) error {
		return client.SetLabels(ctx, cd.Key, strings.Fields(cd.Labels))
	})
	return m.runBulk(i18n.T("labels"), keys, func(ctx context.Context, key string) error {
		return client.EditLabels(ctx, key, add, remove)
	})
}

// bulkWorkers is how many writes run at once: quick, without tripping Jira's
// rate limit.
const bulkWorkers = 4

// runBulk runs write for every key, a few at a time.
func (m *Model) runBulk(what string, keys []string, write func(ctx context.Context, key string) error) tea.Cmd {
	m.status = i18n.Tf("updating %s on %d issues…", what, len(keys))
	ctx, quick := m.ctx, m.quickKey != ""
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
		return bulkDoneMsg{what: what, keys: keys, failed: failed, quick: quick}
	}
}

// handleBulkDone reports the edit, keeps only the failed cards marked and
// refetches the board (and the panel's issue when it was one of them).
func (m Model) handleBulkDone(msg bulkDoneMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if msg.quick {
		// A quick edit leaves the marks alone; one opened since on another
		// card stays open.
		if m.quickKey == msg.keys[0] {
			m.quickKey = ""
		}
		m.status = i18n.Tf("%s set on %s", msg.what, strings.Join(msg.keys, ", "))
		m.noteRefine(m.status)
		for k, err := range msg.failed {
			m.fail(fmt.Sprintf("%s: %s: %s", k, msg.what, err.Error()))
		}
		t.rows = nil
		m.renderJira()
		cmds := []tea.Cmd{m.refreshJiraAfterEdit()}
		if r := m.currentRef(); r != nil && slices.Contains(msg.keys, r.jiraKey) {
			cmds = append(cmds, m.loadCurrentRef())
		}
		if t.plan != nil {
			cmds = append(cmds, m.loadPlan())
		}
		if t.roadmap != nil {
			cmds = append(cmds, m.loadRoadmap())
		}
		return m, tea.Batch(cmds...)
	}
	was := t.marked
	t.marked = nil
	for k := range msg.failed {
		if t.marked == nil {
			t.marked = map[string]jira.Card{}
		}
		t.marked[k] = was[k]
	}
	t.rows = nil
	ok := len(msg.keys) - len(msg.failed)
	m.status = i18n.Tf("%s set on %d", msg.what, ok)
	if len(msg.failed) > 0 {
		for _, k := range slices.Sorted(maps.Keys(msg.failed)) {
			m.logError(k + ": " + msg.what + ": " + msg.failed[k].Error()) // each reason, in messages
		}
		if len(msg.failed) == 1 {
			for k, err := range msg.failed {
				m.fail(m.status + i18n.Tf(" · %s failed (still marked): %v", k, err))
			}
		} else {
			m.fail(m.status + i18n.Tf(" · %d failed (still marked) · %s messages says why", len(msg.failed), helpKey(m.keys.Palette)))
		}
	}
	cmds := []tea.Cmd{m.refreshJiraAfterEdit()}
	if r := m.currentRef(); r != nil && slices.Contains(msg.keys, r.jiraKey) {
		cmds = append(cmds, m.loadCurrentRef())
	}
	if t.plan != nil {
		cmds = append(cmds, m.loadPlan())
	}
	m.renderJira()
	return m, tea.Batch(cmds...)
}
