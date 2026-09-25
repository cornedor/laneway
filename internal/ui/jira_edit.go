package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// jiraAssigneeDebounce delays the assignable-user search after a keystroke so
// rapid typing coalesces into one request (mirrors mentionDebounce).
const jiraAssigneeDebounce = 200 * time.Millisecond

// Editable Jira fields. While the panel has focus (see jira.go) the keys
// s / p / a open a modal list picker for Status / Priority / Assignee, and P
// opens a numeric input for Story points. The pickers and the input are fully
// modal — they own every keystroke while open (dispatched in update.go before
// the focus-based routing) and overlay the screen (view.go), mirroring the
// reaction picker. A confirmed change writes to Jira (internal/jira), then the
// panel refetches the issue so authoritative — and any cascading — values show.

// jiraPickerKind selects which field a picker edits and which fetch/mutation
// backs it.
type jiraPickerKind int

const (
	jiraPickStatus jiraPickerKind = iota
	jiraPickPriority
	jiraPickAssignee
	// jiraPickReplyTarget lists the issue's comments so the user can choose
	// which one to reply to. Unlike the others it triggers no mutation — picking
	// a row opens the reply composer (see applyJiraPick / jira_comment.go).
	jiraPickReplyTarget
	// jiraPickProject and jiraPickBoard switch the Jira tab's board
	// (jiratab.go). The project list filters locally.
	jiraPickProject
	jiraPickBoard
	// jiraPickBoardAssignee filters the Jira tab's board by assignee; it
	// filters locally too.
	jiraPickBoardAssignee
	// jiraPickFormUser and jiraPickFormOption fill a row of the transition
	// form (jira_transition.go): a person searched server-side like the
	// assignee, or one of the field's options, filtered locally.
	jiraPickFormUser
	jiraPickFormOption
	// jiraPickLaneStatus picks which of a lane's statuses a keyboard move
	// lands on (jiratab.go).
	jiraPickLaneStatus
	// jiraPickLink lists the issue's parent, links and subtasks; picking one
	// shows it in the panel.
	jiraPickLink
	// jiraPickCreateType picks a new issue's type, then asks its summary
	// (jira_create.go).
	jiraPickCreateType
	// jiraPickSprint moves the selected card to a sprint or the backlog
	// (jira_sprint.go).
	jiraPickSprint
	// jiraPickPalette is the command palette (palette.go).
	jiraPickPalette
	// jiraPickBulk asks what to change on the marked cards (bulk.go).
	jiraPickBulk
)

// jiraPickerItem is one selectable row. id is the value handed to the mutation
// (transition id / priority id / accountId; "" = unassign). current marks the
// issue's present value with a ✓.
type jiraPickerItem struct {
	id      string
	label   string
	current bool
}

// jiraPickerState is the modal list picker reused for the three list-style
// fields. The assignee picker is filterable: a textinput drives a debounced
// server-side search of assignable users (a large project has more than the
// default page); status and priority are short fixed lists with 1-9
// accelerators. fetchSeq is bumped on every (re)query so a late response from
// an earlier query is discarded — same idea as mentionState.fetchSeq.
type jiraPickerState struct {
	active      bool
	kind        jiraPickerKind
	issueKey    string
	title       string
	gen         int // drops a stale async load (picker reopened/closed since)
	loading     bool
	err         error
	items       []jiraPickerItem
	idx         int  // cursor into the item list
	filterable  bool // assignee: has a search box backed by the server
	filter      textinput.Model
	fetchSeq    int              // discards a search response from a superseded query
	curAssignee string           // accountId of the issue's assignee, to mark ✓ across re-queries
	all         []jiraPickerItem // a locally filtered picker's full list
	bulk        []string         // the marked keys a pick applies to, none for one issue
}

// jiraPickerLoadedMsg carries the fetched option list for an open picker. gen +
// kind + seq drop a result the user has since closed, replaced, or re-queried.
type jiraPickerLoadedMsg struct {
	gen   int
	seq   int
	kind  jiraPickerKind
	items []jiraPickerItem
	err   error
	// projects is the project picker's fetch, kept for the next opening.
	projects []jira.Project
}

// jiraAssigneeDebounceMsg fires after the debounce window to run the pending
// assignee search for seq (ignored if a newer keystroke has superseded it).
type jiraAssigneeDebounceMsg struct{ seq int }

// jiraMutatedMsg carries the result of a field write. On success the panel
// reloads the issue (the client already invalidated its cache).
type jiraMutatedMsg struct {
	key   string
	field string
	err   error
}

// startJiraPicker resets the picker to a fresh loading state for the current
// issue and returns the new generation so the caller's fetch can be matched
// against it.
func (m *Model) startJiraPicker(kind jiraPickerKind, title string, filterable bool) int {
	gen := m.jiraPicker.gen + 1
	issueKey := ""
	if m.jiraIssue != nil {
		issueKey = m.jiraIssue.Key
	}
	m.jiraPicker = jiraPickerState{
		active:     true,
		kind:       kind,
		issueKey:   issueKey,
		title:      title,
		gen:        gen,
		loading:    true,
		filterable: filterable,
		fetchSeq:   1,
	}
	if filterable {
		ti := textinput.New()
		ti.Prompt = "❯ "
		ti.Placeholder = "filter…"
		ti.SetWidth(40)
		ti.Focus()
		m.jiraPicker.filter = ti
	}
	return gen
}

// openJiraStatusPicker loads the issue's workflow transitions (the only status
// changes Jira accepts) into the picker.
func (m *Model) openJiraStatusPicker() tea.Cmd {
	gen := m.startJiraPicker(jiraPickStatus, "Set status — "+m.jiraIssue.Key, false)
	seq := m.jiraPicker.fetchSeq
	key, cur := m.jiraIssue.Key, m.jiraIssue.Status
	client, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		opts, err := client.Transitions(ctx, key)
		if err != nil {
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStatus, err: err}
		}
		items := make([]jiraPickerItem, 0, len(opts))
		for _, o := range opts {
			items = append(items, jiraPickerItem{id: o.ID, label: o.Name, current: o.Name == cur})
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStatus, items: items}
	}
}

// openJiraPriorityPicker loads the instance priority list into the picker.
func (m *Model) openJiraPriorityPicker() tea.Cmd {
	gen := m.startJiraPicker(jiraPickPriority, "Set priority — "+m.jiraIssue.Key, false)
	seq := m.jiraPicker.fetchSeq
	curID := m.jiraIssue.PriorityID
	client, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		opts, err := client.Priorities(ctx)
		if err != nil {
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickPriority, err: err}
		}
		items := make([]jiraPickerItem, 0, len(opts))
		for _, o := range opts {
			items = append(items, jiraPickerItem{id: o.ID, label: o.Name, current: o.ID == curID})
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickPriority, items: items}
	}
}

// openJiraAssigneePicker opens the filterable assignee picker and runs the
// initial (empty-query) search. Typing re-runs it server-side (see
// handleJiraPickerKey / jiraAssigneeDebounceMsg).
func (m *Model) openJiraAssigneePicker() tea.Cmd {
	gen := m.startJiraPicker(jiraPickAssignee, "Set assignee — "+m.jiraIssue.Key, true)
	m.jiraPicker.curAssignee = m.jiraIssue.AssigneeAccountID
	return m.fetchAssignees(gen, m.jiraPicker.fetchSeq, m.jiraIssue.Key, "")
}

// fetchAssignees searches assignable users for query and builds the picker
// rows. With an empty query (the default view) it prepends Unassigned and
// "Assign to me"; while searching it shows server matches only.
func (m Model) fetchAssignees(gen, seq int, key, query string) tea.Cmd {
	client, ctx := m.jiraClient, m.ctx
	curID, kind := m.jiraPicker.curAssignee, m.jiraPicker.kind
	return func() tea.Msg {
		users, err := client.AssignableUsers(ctx, key, query)
		if err != nil {
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: kind, err: err}
		}
		var items []jiraPickerItem
		meID := ""
		if strings.TrimSpace(query) == "" {
			items = append(items, jiraPickerItem{id: "", label: "Unassigned", current: curID == ""})
			// "Assign to me" is a convenience pinned near the top; a Myself
			// failure just omits it (and the dedup below is skipped).
			if me, meErr := client.Myself(ctx); meErr == nil && me.AccountID != "" {
				meID = me.AccountID
				items = append(items, jiraPickerItem{
					id:      me.AccountID,
					label:   "Assign to me (" + me.DisplayName + ")",
					current: me.AccountID == curID,
				})
			}
		}
		for _, u := range users {
			if meID != "" && u.AccountID == meID {
				continue // already shown as "Assign to me"
			}
			items = append(items, jiraPickerItem{id: u.AccountID, label: u.DisplayName, current: u.AccountID == curID})
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: kind, items: items}
	}
}

// openJiraReplyPicker lists the issue's comments (newest first) so the user can
// pick which one to reply to. The comments are already loaded with the issue,
// so the picker opens populated — there's no fetch. Each row's id is the index
// into m.jiraIssue.Comments, so applyJiraPick can recover the chosen comment.
func (m *Model) openJiraReplyPicker() {
	m.startJiraPicker(jiraPickReplyTarget, "Reply to comment — "+m.jiraIssue.Key, false)
	items := make([]jiraPickerItem, 0, len(m.jiraIssue.Comments))
	for i := len(m.jiraIssue.Comments) - 1; i >= 0; i-- {
		items = append(items, jiraPickerItem{id: strconv.Itoa(i), label: commentPickerLabel(m.jiraIssue.Comments[i])})
	}
	m.jiraPicker.loading = false
	m.jiraPicker.items = items
	m.jiraPicker.idx = 0
}

// commentPickerLabel is a one-line "Author: snippet" used in the reply picker.
func commentPickerLabel(c jira.Comment) string {
	author := c.Author
	if author == "" {
		author = "Unknown"
	}
	snippet := strings.Join(strings.Fields(c.Body), " ")
	const max = 60
	if r := []rune(snippet); len(r) > max {
		snippet = string(r[:max]) + "…"
	}
	if snippet == "" {
		return author
	}
	return author + ": " + snippet
}

// openJiraPointsInput shows the numeric story-points input seeded with the
// current value (empty when unset).
func (m *Model) openJiraPointsInput() {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "number (empty clears)"
	ti.CharLimit = 12
	ti.SetWidth(24)
	ti.SetValue(m.jiraIssue.StoryPoints)
	ti.CursorEnd()
	ti.Focus()
	m.jiraFieldInput = ti
	m.jiraFieldActive = true
	m.jiraFieldName = "points"
	m.jiraFieldKey = m.jiraIssue.Key
}

// openJiraSummaryInput shows the summary input seeded with the current one.
func (m *Model) openJiraSummaryInput() {
	m.openJiraTextInput("summary", m.jiraIssue.Summary, "", 255) // Jira's summary limit
}

// openJiraLabelsInput shows the labels, space separated: a label has no
// spaces.
func (m *Model) openJiraLabelsInput() {
	m.openJiraTextInput("labels", strings.Join(m.jiraIssue.Labels, " "), "space separated (empty clears)", 0)
}

// openJiraTextInput opens the wide field input for field, seeded with value.
func (m *Model) openJiraTextInput(field, value, placeholder string, limit int) {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = placeholder
	ti.CharLimit = limit
	ti.SetWidth(max(min(m.width-16, 72), 16))
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Focus()
	m.jiraFieldInput = ti
	m.jiraFieldActive = true
	m.jiraFieldName = field
	m.jiraFieldKey = m.jiraIssue.Key
}

// handleJiraPickerLoaded installs a finished option fetch and parks the cursor
// on the current value (when present). Stale results are dropped.
func (m Model) handleJiraPickerLoaded(msg jiraPickerLoadedMsg) (tea.Model, tea.Cmd) {
	if !m.jiraPicker.active || msg.gen != m.jiraPicker.gen || msg.kind != m.jiraPicker.kind || msg.seq != m.jiraPicker.fetchSeq {
		return m, nil
	}
	if msg.projects != nil {
		m.jiraTab.projects = msg.projects
	}
	m.jiraPicker.err = msg.err
	m.setJiraPickerItems(msg.items)
	return m, nil
}

// setJiraPickerItems fills an open picker and parks the cursor on the current
// value (when present).
func (m *Model) setJiraPickerItems(items []jiraPickerItem) {
	m.jiraPicker.loading = false
	m.jiraPicker.items = items
	m.jiraPicker.all = items
	m.jiraPicker.idx = 0
	for i, it := range items {
		if it.current {
			m.jiraPicker.idx = i
			break
		}
	}
}

// filterJiraPicker narrows a locally filtered picker to rows containing
// every word of the filter.
func (m *Model) filterJiraPicker() {
	terms := strings.Fields(strings.ToLower(m.jiraPicker.filter.Value()))
	m.jiraPicker.items = nil
	for _, it := range m.jiraPicker.all {
		label := strings.ToLower(it.label)
		if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(label, t) }) {
			m.jiraPicker.items = append(m.jiraPicker.items, it)
		}
	}
	m.jiraPicker.idx = 0
}

// closeJiraPicker tears the picker down, preserving gen so a late load is
// ignored.
func (m *Model) closeJiraPicker() {
	m.jiraPicker = jiraPickerState{gen: m.jiraPicker.gen}
}

// closeJiraField tears the field input down.
func (m *Model) closeJiraField() {
	m.jiraFieldActive = false
	m.jiraFieldName = ""
	m.jiraFieldKey = ""
	m.jiraFieldInput = textinput.Model{}
}

// handleJiraPickerKey owns every keystroke while the list picker is open.
// Arrow/ctrl-nav move the cursor; for the fixed lists 1-9 jump+apply; enter
// applies; esc cancels. For the assignee picker, other keys feed the filter.
func (m Model) handleJiraPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.closeJiraPicker()
		return m, nil
	case "enter":
		return m.applyJiraPick()
	}

	if m.jiraPicker.filterable {
		// Arrows + ctrl+p/ctrl+n navigate so letters stay available for typing.
		switch {
		case key.Matches(msg, m.keys.InputUp):
			m.jiraPickerMove(-1)
			return m, nil
		case key.Matches(msg, m.keys.InputDown):
			m.jiraPickerMove(1)
			return m, nil
		}
		before := m.jiraPicker.filter.Value()
		var cmd tea.Cmd
		m.jiraPicker.filter, cmd = m.jiraPicker.filter.Update(msg)
		if m.jiraPicker.filter.Value() == before {
			return m, cmd
		}
		if k := m.jiraPicker.kind; k == jiraPickProject || k == jiraPickBoardAssignee || k == jiraPickFormOption || k == jiraPickPalette {
			m.filterJiraPicker()
			return m, cmd
		}
		// Query changed: schedule a debounced server search. fetchSeq drops any
		// earlier in-flight search. The existing rows stay on screen meanwhile.
		m.jiraPicker.fetchSeq++
		seq := m.jiraPicker.fetchSeq
		debounce := tea.Tick(jiraAssigneeDebounce, func(time.Time) tea.Msg {
			return jiraAssigneeDebounceMsg{seq: seq}
		})
		return m, tea.Batch(cmd, debounce)
	}

	switch {
	case key.Matches(msg, m.keys.Up):
		m.jiraPickerMove(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.jiraPickerMove(1)
		return m, nil
	}
	// Digit accelerators 1..9 over the (short) fixed list.
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		idx := int(s[0] - '1')
		if idx < len(m.jiraPicker.items) {
			m.jiraPicker.idx = idx
			return m.applyJiraPick()
		}
	}
	return m, nil
}

// handleJiraAssigneeDebounce runs the pending assignee search once the debounce
// window elapses, unless a newer keystroke has superseded it.
func (m Model) handleJiraAssigneeDebounce(msg jiraAssigneeDebounceMsg) (tea.Model, tea.Cmd) {
	if k := m.jiraPicker.kind; !m.jiraPicker.active || (k != jiraPickAssignee && k != jiraPickFormUser) || msg.seq != m.jiraPicker.fetchSeq {
		return m, nil
	}
	return m, m.fetchAssignees(m.jiraPicker.gen, msg.seq, m.jiraPicker.issueKey, m.jiraPicker.filter.Value())
}

// jiraPickerMove nudges the cursor within the item list, clamped to bounds. The
// render windows around it (see renderJiraPicker), so no scroll state is tracked
// here.
func (m *Model) jiraPickerMove(delta int) {
	n := len(m.jiraPicker.items)
	if n == 0 {
		m.jiraPicker.idx = 0
		return
	}
	idx := m.jiraPicker.idx + delta
	if idx < 0 {
		idx = 0
	}
	if idx > n-1 {
		idx = n - 1
	}
	m.jiraPicker.idx = idx
}

// handleJiraFieldKey owns every keystroke while the field input is open.
func (m Model) handleJiraFieldKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.closeJiraField()
		return m, nil
	case "enter":
		return m.applyJiraField()
	}
	var cmd tea.Cmd
	m.jiraFieldInput, cmd = m.jiraFieldInput.Update(msg)
	return m, cmd
}

// applyJiraPick closes the picker and fires the mutation for the highlighted
// row.
func (m Model) applyJiraPick() (tea.Model, tea.Cmd) {
	items := m.jiraPicker.items
	if m.jiraPicker.idx < 0 || m.jiraPicker.idx >= len(items) {
		return m, nil
	}
	it := items[m.jiraPicker.idx]
	kind := m.jiraPicker.kind

	// The reply-target picker mutates nothing: the chosen row opens the reply
	// composer prefilled from the loaded comment (id is its index).
	if kind == jiraPickReplyTarget {
		m.closeJiraPicker()
		idx, err := strconv.Atoi(it.id)
		if err != nil || m.jiraIssue == nil || idx < 0 || idx >= len(m.jiraIssue.Comments) {
			return m, nil
		}
		m.openJiraReply(m.jiraIssue.Comments[idx])
		return m, nil
	}

	if kind == jiraPickProject || kind == jiraPickBoard {
		m.closeJiraPicker()
		return m, m.pickJiraBoard(kind, it.id)
	}
	if bulk := m.jiraPicker.bulk; len(bulk) > 0 {
		m.closeJiraPicker()
		return m, m.applyBulkPick(kind, bulk, it)
	}
	if kind == jiraPickBulk {
		m.closeJiraPicker()
		return m, m.applyBulkMenu(it.id)
	}
	if kind == jiraPickPalette {
		m.closeJiraPicker()
		return m.applyPalette(it.id)
	}
	if kind == jiraPickCreateType {
		m.closeJiraPicker()
		m.openJiraCreateSummary(it.id)
		return m, nil
	}
	if kind == jiraPickBoardAssignee {
		m.closeJiraPicker()
		return m, m.setJiraAssignee(it.id, it.label)
	}
	if kind == jiraPickFormUser || kind == jiraPickFormOption {
		key := m.jiraPicker.issueKey
		m.closeJiraPicker()
		if m.jiraForm == nil {
			return m, m.pickPanelExtra(key, kind, it)
		}
		m.pickJiraFormValue(kind, it)
		return m, nil
	}
	if kind == jiraPickLaneStatus {
		m.closeJiraPicker()
		return m, m.pickJiraLaneStatus(it.id)
	}
	if kind == jiraPickLink {
		m.closeJiraPicker()
		m.selectJiraKey(it.id)
		m.renderJira()
		return m.openJiraKey(it.id)
	}
	if kind == jiraPickSprint {
		key := m.jiraPicker.issueKey
		m.closeJiraPicker()
		return m, m.moveJiraToSprint([]string{key}, it)
	}
	if kind == jiraPickStatus {
		key := m.jiraPicker.issueKey
		m.closeJiraPicker()
		return m, m.startJiraMoveFromPanel(key, it.id, it.label)
	}

	key := m.jiraPicker.issueKey
	m.closeJiraPicker()

	client, ctx := m.jiraClient, m.ctx
	var field string
	var run func() error
	switch kind {
	case jiraPickStatus:
		field = "status"
		run = func() error { return client.DoTransition(ctx, key, it.id) }
	case jiraPickPriority:
		field = "priority"
		run = func() error { return client.SetPriority(ctx, key, it.id) }
	case jiraPickAssignee:
		field = "assignee"
		run = func() error { return client.SetAssignee(ctx, key, it.id) }
	default:
		return m, nil
	}
	m.status = fmt.Sprintf("updating %s %s…", key, field)
	return m, jiraMutateCmd(key, field, run)
}

// applyJiraField closes the input and fires the field's write. An empty
// summary is refused: Jira requires one.
func (m Model) applyJiraField() (tea.Model, tea.Cmd) {
	key, field := m.jiraFieldKey, m.jiraFieldName
	raw := m.jiraFieldInput.Value()
	client, ctx := m.jiraClient, m.ctx
	run := func() error { return client.SetStoryPoints(ctx, key, raw) }
	if field == "summary" {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			m.status = "a summary can't be empty"
			return m, nil
		}
		if m.jiraIssue != nil && m.jiraIssue.Key == key && raw == m.jiraIssue.Summary {
			m.closeJiraField()
			return m, nil
		}
		run = func() error { return client.SetSummary(ctx, key, raw) }
	}
	if strings.HasPrefix(field, "bulk-") {
		return m.applyBulkField(field, raw)
	}
	if field == "field" {
		return m.applyPanelExtraText(raw)
	}
	if field == "labels" {
		labels := strings.Fields(raw)
		if m.jiraIssue != nil && m.jiraIssue.Key == key && slices.Equal(labels, m.jiraIssue.Labels) {
			m.closeJiraField()
			return m, nil
		}
		run = func() error { return client.SetLabels(ctx, key, labels) }
	}
	m.closeJiraField()
	m.status = fmt.Sprintf("updating %s %s…", key, field)
	return m, jiraMutateCmd(key, field, run)
}

// jiraMutateCmd runs a field write in the background and reports the result.
func jiraMutateCmd(key, field string, run func() error) tea.Cmd {
	return func() tea.Msg {
		return jiraMutatedMsg{key: key, field: field, err: run()}
	}
}

// handleJiraMutated reports the write outcome and reloads the issue on success
// so the panel shows the authoritative (and any cascading) values.
func (m Model) handleJiraMutated(msg jiraMutatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("%s %s update failed: %v", msg.key, msg.field, msg.err)
		return m, nil
	}
	m.status = fmt.Sprintf("%s %s updated", msg.key, msg.field)
	board := m.refreshJiraAfterEdit()
	if r := m.currentRef(); r != nil && r.jiraKey == msg.key {
		return m, tea.Batch(m.loadCurrentRef(), board)
	}
	return m, board
}

// renderJiraPicker draws the modal list picker. Layout mirrors
// renderReactionPicker (rounded border, centred title, ✓ on the current value,
// cursor row in focusedColor, footer hint); a long list is windowed around the
// selection the same way renderSwitcherCommands does, so it never overflows
// maxH (the body area height passed by view.go).
func (m *Model) renderJiraPicker(maxH int) string {
	if !m.jiraPicker.active {
		return ""
	}
	outerW := confirmDialogMaxWidth
	if outerW > m.width-4 {
		outerW = m.width - 4
	}
	if outerW < 32 {
		outerW = 32
	}
	inner := outerW - 8
	if inner < 1 {
		inner = 1
	}

	parts := []string{lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).Render(m.jiraPicker.title)}
	if m.jiraPicker.filterable {
		parts = append(parts, m.jiraPicker.filter.View())
	}

	switch {
	case m.jiraPicker.loading:
		parts = append(parts, "", refDimStyle.Render("loading…"))
	case m.jiraPicker.err != nil:
		parts = append(parts, "", refErrStyle.Render(m.jiraPicker.err.Error()))
	default:
		vis := m.jiraPicker.items
		if len(vis) == 0 {
			parts = append(parts, "", refDimStyle.Render("no matches"))
			break
		}
		// Window a long set (e.g. assignable users) around the selection so the
		// popup stays within maxH, mirroring renderSwitcherCommands. Chrome inside
		// the box is the title + blank + two scroll markers + blank + hint (6) plus
		// the border + padding (4), plus the filter input when present.
		win := maxH - 10
		if m.jiraPicker.filterable {
			win--
		}
		if win < 3 {
			win = 3
		}
		start := 0
		if len(vis) > win {
			// Keep the selected row roughly centred, clamped to the ends.
			start = m.jiraPicker.idx - win/2
			if start < 0 {
				start = 0
			}
			if start > len(vis)-win {
				start = len(vis) - win
			}
		}
		end := start + win
		if end > len(vis) {
			end = len(vis)
		}

		cursorStyle := lipgloss.NewStyle().Foreground(focusedColor).Bold(true)
		rows := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			it := vis[i]
			prefix := ""
			if !m.jiraPicker.filterable {
				accel := " "
				if i < 9 {
					accel = fmt.Sprintf("%d", i+1)
				}
				prefix = "[" + accel + "] "
			}
			marker := " "
			if it.current {
				marker = "✓"
			}
			text := fmt.Sprintf("%s%s %s", prefix, marker, it.label)
			if i == m.jiraPicker.idx {
				rows = append(rows, cursorStyle.Render("▸ "+text))
			} else {
				rows = append(rows, "  "+text)
			}
		}

		// Scroll markers, shown only when there's something off-screen (win is
		// sized assuming both can appear, so the popup stays within maxH).
		parts = append(parts, "")
		if start > 0 {
			parts = append(parts, refDimStyle.Render(fmt.Sprintf("  ↑ %d more", start)))
		}
		parts = append(parts, strings.Join(rows, "\n"))
		if end < len(vis) {
			parts = append(parts, refDimStyle.Render(fmt.Sprintf("  ↓ %d more", len(vis)-end)))
		}
	}

	hintTxt := "↑/↓ move · ↵ apply · esc cancel"
	if m.jiraPicker.filterable {
		hintTxt = "type to filter · ↑/↓ move · ↵ apply · esc cancel"
	}
	parts = append(parts, "", lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).Render(hintTxt))

	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// renderJiraFieldInput draws the story points or summary modal.
func (m *Model) renderJiraFieldInput() string {
	if !m.jiraFieldActive {
		return ""
	}
	title, hint, outerW := "Set story points", "↵ save · empty clears · esc cancel", 40
	switch m.jiraFieldName {
	case "summary":
		title, hint, outerW = "Edit summary", "↵ save · esc cancel", m.jiraFieldInput.Width()+12
	case "labels":
		title, hint, outerW = "Edit labels", "↵ save · empty clears · esc cancel", m.jiraFieldInput.Width()+12
	case "bulk-labels":
		title, hint, outerW = "Edit labels", "↵ save · esc cancel", m.jiraFieldInput.Width()+12
	case "bulk-points":
		title = "Set story points"
	case "field":
		title, hint, outerW = "Edit "+m.panelEditField().Name, "↵ save · empty clears · esc cancel", m.jiraFieldInput.Width()+12
	}
	if outerW > m.width-4 {
		outerW = m.width - 4
	}
	if outerW < 24 {
		outerW = 24
	}
	inner := outerW - 8
	if inner < 1 {
		inner = 1
	}
	header := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).Render(title + " — " + m.jiraFieldKey)
	hint = lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).Render(hint)
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", m.jiraFieldInput.View(), "", hint)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).Render(body)
}
