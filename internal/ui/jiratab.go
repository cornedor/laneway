package ui

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/safeterm"
	"github.com/cornedor/laneway/internal/viewport"
)

// The board: one board of one project, as swim lanes or a list, with the
// selected issue opening in the reference panel on the right.

// jiraMetaPrefix keys the tab's remembered choices in the store's meta table:
// project, board:<project>, mode, assignee and quick:<board>.
const jiraMetaPrefix = "jira_tab:"

// Card geometry in the lane view: three lines and a gap.
const (
	jiraCardH    = 3
	jiraCardSlot = jiraCardH + 1
	jiraLaneMinW = 22
)

// jiraBodyTop is the screen row of the first body line: the title row, its
// rule, the view selector and the filter line.
const jiraBodyTop = 4

type jiraViewKind int

const (
	jiraViewBoard jiraViewKind = iota // a kanban board
	jiraViewSprint
	jiraViewBacklog
	jiraViewJQL // a ui.views entry: the board's issues narrowed by jql
)

// jiraView is one issue list of a board: a sprint, the kanban board, the
// backlog, or a configured JQL view. lanes is whether it may show as swim lanes; planning lists
// (backlog, future sprints) are list-only.
type jiraView struct {
	kind   jiraViewKind
	name   string
	sprint int
	jql    string
	lanes  bool
	// A sprint's dates (zero until planned) and goal.
	start, end time.Time
	goal       string
}

// jiraLane is a board column and the cards (indexes into cards) in it.
type jiraLane struct {
	name      string
	statusIDs []string
	cards     []int
	max       int // the column's WIP limit, 0 for none
}

// jiraAssignee is the board's assignee filter: id "" for everyone, "me",
// "none" for unassigned, else an accountId.
type jiraAssignee struct {
	id    string
	label string
}

// jiraFilterJQL is the JQL the filters narrow a view by: the assignee and
// every quick filter that is on, all of which must hold (as on Jira's board).
func jiraFilterJQL(a jiraAssignee, quick []jira.QuickFilter, on map[int]bool) string {
	var parts []string
	switch a.id {
	case "":
	case "me":
		parts = append(parts, "assignee = currentUser()")
	case "none":
		parts = append(parts, "assignee is EMPTY")
	default:
		parts = append(parts, fmt.Sprintf("assignee = %q", a.id))
	}
	for _, q := range quick {
		if on[q.ID] {
			parts = append(parts, "("+q.JQL+")")
		}
	}
	return strings.Join(parts, " AND ")
}

// withLocalQuick puts the config's presets (negative ids) before the
// board's quick filters, replacing any already there.
func withLocalQuick(board, local []jira.QuickFilter) []jira.QuickFilter {
	out := append([]jira.QuickFilter(nil), local...)
	for _, q := range board {
		if q.ID >= 0 {
			out = append(out, q)
		}
	}
	return out
}

// andJQL joins two JQL clauses, either of which may be empty.
func andJQL(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return "(" + a + ") AND (" + b + ")"
}

// jiraDrag is a card being dragged between lanes with the mouse. A click arms
// it (key set); it goes active once the pointer moves past a small threshold.
type jiraDrag struct {
	active bool
	key    string
	x, y   int // where the click armed it
	from   int
	over   int
	zone   int // the status zone under the pointer, in a lane of several
}

type jiraTabState struct {
	view     viewport.Model // the list mode's rows
	project  string
	projects []jira.Project // the picker's list, fetched once
	boards   []jira.Board
	board    int // index into boards
	cfg      *jira.BoardConfig
	views    []jiraView
	// rulesSeen is each board, view and filter's last fresh cards, for rules.
	rulesSeen map[string][]jira.Card
	// highlights are cards a rule marked, by key: the colour, until opened.
	highlights map[string]string
	// marked are the cards a bulk edit applies to (bulk.go), by key.
	marked    map[string]bool
	viewIdx   int
	wantLanes bool // the user's mode; a list-only view overrides it
	modeRead  bool // wantLanes and the assignee were restored from the store

	assignee jiraAssignee
	quick    []jira.QuickFilter
	quickOn  map[int]bool      // quick filter id → on
	people   map[string]string // accountId → name, seen on this board
	// statusNames names status ids, for a lane's drop zones.
	statusNames map[string]string
	// pendingMove is a keyboard move into a lane of several statuses,
	// waiting on the status picker.
	pendingMove struct {
		key  string
		lane int
	}

	cards []jira.Card
	total int
	lanes []jiraLane
	order []int // the list mode's row order, indexes into cards
	// rows caches the list mode's unselected rows, per order, for rowsFor;
	// buildJiraLanes drops it.
	rows    []string
	rowsFor [3]int // width, key and status column widths

	idx       int   // list cursor, into order
	lane, row int   // lane cursor
	laneTop   []int // per lane, the first card on screen
	firstLane int   // the first lane on screen
	laneW     int   // a lane's width in the last render
	lanesOut  string

	sort jiraSort // the list's order; lanes keep the board's rank

	// search narrows the cards locally; searching while it has the keyboard.
	search    textinput.Model
	searching bool
	// roadmap shows in place of the cards while non-nil (roadmap.go).
	roadmap *roadmapState

	drag    jiraDrag
	loading bool
	err     string
	seq     int
	fresh   int // the last seq the network answered, which a cached copy can't undo
	fetched time.Time
}

// newJiraTabState is held by pointer, like the GitLab tab's.
func newJiraTabState() *jiraTabState {
	return &jiraTabState{view: viewport.New(), wantLanes: true}
}

// jiraBoardMsg carries a full board load: project, boards, columns, views and
// the selected view's cards. Stale responses (seq behind) are dropped.
type jiraBoardMsg struct {
	seq      int
	cached   bool // from the store, the network copy still coming
	project  string
	boards   []jira.Board
	board    int
	cfg      *jira.BoardConfig
	views    []jiraView
	viewIdx  int
	lanes    *bool // the stored mode, on the first load
	quick    []jira.QuickFilter
	quickOn  map[int]bool
	assignee jiraAssignee
	// statusNames names status ids, for the drop zones.
	statusNames map[string]string
	cards       []jira.Card
	total       int
	err         error
}

// jiraCardsMsg carries one view's cards, after a view switch or a move.
type jiraCardsMsg struct {
	seq     int
	cached  bool
	viewIdx int
	cards   []jira.Card
	total   int
	err     error
}

// jiraMovedMsg reports a card's transition to another lane.
type jiraMovedMsg struct {
	key  string
	lane string
	err  error
}

// enterJiraTab focuses the board and refetches when it is missing or older
// than the stale_after option.
func (m *Model) enterJiraTab() tea.Cmd {
	m.focus = focusJira
	t := m.jiraTab
	if t.loading || (t.cfg != nil && time.Since(t.fetched) < m.opts.staleAfter) {
		return nil
	}
	// Nothing on screen yet: show the stored copy while the fresh one loads.
	return m.loadJiraBoard(t.project, m.jiraBoardID(), m.jiraViewName(), t.cfg == nil)
}

func (m *Model) jiraBoardID() int {
	t := m.jiraTab
	if t.board >= 0 && t.board < len(t.boards) {
		return t.boards[t.board].ID
	}
	return 0
}

func (m *Model) jiraViewName() string {
	if v, ok := m.jiraCurrentView(); ok {
		return v.name
	}
	return ""
}

func (m *Model) jiraCurrentView() (jiraView, bool) {
	t := m.jiraTab
	if t.viewIdx >= 0 && t.viewIdx < len(t.views) {
		return t.views[t.viewIdx], true
	}
	return jiraView{}, false
}

// jiraShowsLanes reports whether the board shows as swim lanes right now.
func (m *Model) jiraShowsLanes() bool {
	v, ok := m.jiraCurrentView()
	return ok && v.lanes && m.jiraTab.wantLanes
}

// loadJiraBoard fetches a board from scratch. An empty project falls back to
// the remembered one, then the first configured, then the first visible;
// board 0 to the remembered board of the project, then its first; view keeps
// a view by name across a refresh, and an empty one opens the board's last.
// fromCache shows the stored copy of the board first.
func (m *Model) loadJiraBoard(project string, boardID int, view string, fromCache bool) tea.Cmd {
	t := m.jiraTab
	t.seq++
	t.loading = true
	t.err = ""
	m.renderJira()
	seq, ctx, st, c := t.seq, m.ctx, m.store, m.jiraClient
	configured := m.jiraProjects
	readMode := !t.modeRead
	assignee, quickOn, quickBoard, local, localViews := t.assignee, t.quickOn, m.jiraBoardID(), m.opts.quick, m.opts.views
	var cached tea.Cmd
	if fromCache {
		cached = jiraBoardFromCache(st, seq, project, boardID, view, configured, readMode)
	}
	return tea.Batch(cached, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		msg := jiraBoardMsg{seq: seq}
		if readMode {
			msg.lanes = storedJiraMode(st)
			if v, ok, _ := st.GetMeta(jiraMetaPrefix + "assignee"); ok {
				id, label, _ := strings.Cut(v, "\t")
				assignee = jiraAssignee{id: id, label: label}
			}
		}
		project, boardID = resolveJiraBoard(st, project, boardID, configured)
		if project == "" {
			ps, err := c.ListProjects(ctx)
			if err != nil {
				msg.err = err
				return msg
			}
			if len(ps) == 0 {
				msg.err = fmt.Errorf("jira: no projects visible")
				return msg
			}
			project = ps[0].Key
		}
		msg.project = project
		boards, err := c.Boards(ctx, project)
		if err != nil {
			msg.err = err
			return msg
		}
		if len(boards) == 0 {
			msg.err = fmt.Errorf("jira: %s has no boards", project)
			return msg
		}
		msg.boards = boards
		for i, b := range boards {
			if b.ID == boardID {
				msg.board = i
			}
		}
		board := boards[msg.board]
		// What the board is made of, fetched side by side; only the columns
		// are essential.
		var (
			cfg                *jira.BoardConfig
			sprints            []jira.Sprint
			cfgErr, sprintsErr error
			wg                 sync.WaitGroup
		)
		wg.Add(4)
		go func() { defer wg.Done(); cfg, cfgErr = c.BoardConfiguration(ctx, board.ID) }()
		go func() {
			defer wg.Done()
			if board.Type == "scrum" {
				sprints, sprintsErr = c.Sprints(ctx, board.ID)
			}
		}()
		go func() { defer wg.Done(); msg.quick, _ = c.QuickFilters(ctx, board.ID) }()
		go func() { defer wg.Done(); msg.statusNames, _ = c.StatusNames(ctx) }()
		wg.Wait()
		if err := firstErr(cfgErr, sprintsErr); err != nil {
			msg.err = err
			return msg
		}
		msg.cfg = cfg
		if board.Type == "scrum" {
			for _, s := range sprints {
				msg.views = append(msg.views, jiraView{kind: jiraViewSprint, name: s.Name, sprint: s.ID, lanes: s.State == "active",
					start: s.Start, end: s.End, goal: s.Goal})
			}
			msg.views = append(msg.views, jiraView{kind: jiraViewBacklog, name: "Backlog"})
		} else {
			msg.views = append(msg.views, jiraView{kind: jiraViewBoard, name: "Board", lanes: true})
			if kanbanBacklog(cfg) >= 0 {
				msg.views = append(msg.views, jiraView{kind: jiraViewBacklog, name: "Backlog"})
			}
		}
		msg.views = append(msg.views, localViews...)
		if view == "" {
			view, _, _ = st.GetMeta(jiraViewKey(board.ID))
		}
		for i, v := range msg.views {
			if v.name == view {
				msg.viewIdx = i
			}
		}
		if board.ID != quickBoard {
			quickOn = map[int]bool{}
			if v, ok, _ := st.GetMeta(jiraMetaPrefix + "quick:" + strconv.Itoa(board.ID)); ok {
				for _, f := range strings.Split(v, ",") {
					if id, err := strconv.Atoi(f); err == nil {
						quickOn[id] = true
					}
				}
			}
		}
		msg.assignee, msg.quickOn = assignee, quickOn
		msg.quick = withLocalQuick(msg.quick, local)
		filter := jiraFilterJQL(assignee, msg.quick, quickOn)
		msg.cards, msg.total, msg.err = fetchJiraView(ctx, c, board.ID, cfg, msg.views[msg.viewIdx], filter)
		_ = st.SetMeta(jiraMetaPrefix+"project", project)
		_ = st.SetMeta(jiraMetaPrefix+"board:"+project, strconv.Itoa(board.ID))
		if msg.err == nil {
			saveJiraCache(st, board.ID, msg.views[msg.viewIdx].name, cacheOf(msg, filter))
		}
		return msg
	})
}

// kanbanBacklog is the index of a kanban board's backlog column (Jira names it
// "Backlog" when the board has one), or -1.
func kanbanBacklog(cfg *jira.BoardConfig) int {
	if cfg != nil && len(cfg.Columns) > 0 && strings.EqualFold(cfg.Columns[0].Name, "backlog") {
		return 0
	}
	return -1
}

// jiraKanbanJQL hides what Jira's own kanban board hides: work done more than
// two weeks ago.
const jiraKanbanJQL = "statusCategory != Done OR updated >= -14d"

func fetchJiraView(ctx context.Context, c *jira.Client, board int, cfg *jira.BoardConfig, v jiraView, filter string) ([]jira.Card, int, error) {
	switch v.kind {
	case jiraViewSprint:
		return c.SprintIssues(ctx, board, v.sprint, filter, cfg.PointsField)
	case jiraViewBacklog:
		return c.BacklogIssues(ctx, board, filter, cfg.PointsField)
	case jiraViewJQL:
		return c.BoardIssues(ctx, board, andJQL(v.jql, filter), cfg.PointsField)
	}
	cards, total, err := c.BoardIssues(ctx, board, andJQL(jiraKanbanJQL, filter), cfg.PointsField)
	if i := kanbanBacklog(cfg); i >= 0 && err == nil {
		kept := cards[:0]
		for _, cd := range cards {
			if !slices.Contains(cfg.Columns[i].StatusIDs, cd.StatusID) {
				kept = append(kept, cd)
			}
		}
		total -= len(cards) - len(kept)
		cards = kept
	}
	return cards, total, err
}

// loadJiraCards refetches the cards of view idx on the loaded board.
// fromCache shows the stored copy first, when it was narrowed by the same
// filters; a refetch after a change leaves it out, as it predates the change.
func (m *Model) loadJiraCards(idx int, fromCache bool) tea.Cmd {
	t := m.jiraTab
	if t.cfg == nil || idx < 0 || idx >= len(t.views) {
		return nil
	}
	t.seq++
	t.loading = true
	t.err = ""
	seq, ctx, c, board, cfg, v := t.seq, m.ctx, m.jiraClient, m.jiraBoardID(), t.cfg, t.views[idx]
	filter := jiraFilterJQL(t.assignee, t.quick, t.quickOn)
	st := m.store
	base := cacheOf(jiraBoardMsg{project: t.project, boards: t.boards, board: t.board, cfg: cfg, views: t.views,
		viewIdx: idx, quick: t.quick, quickOn: t.quickOn, assignee: t.assignee, statusNames: t.statusNames}, filter)
	var cached tea.Cmd
	if fromCache {
		cached = func() tea.Msg {
			if c, ok := readJiraCache(st, board, v.name); ok && c.Filter == filter {
				return jiraCardsMsg{seq: seq, cached: true, viewIdx: idx, cards: c.Cards, total: c.Total}
			}
			return nil
		}
	}
	return tea.Batch(cached, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cards, total, err := fetchJiraView(ctx, c, board, cfg, v, filter)
		if err == nil {
			base.Cards, base.Total = cards, total
			saveJiraCache(st, board, v.name, base)
		}
		return jiraCardsMsg{seq: seq, viewIdx: idx, cards: cards, total: total, err: err}
	})
}

func (m Model) handleJiraBoard(msg jiraBoardMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if msg.seq != t.seq || (msg.cached && t.fresh == msg.seq) {
		return m, nil
	}
	if !msg.cached {
		t.loading, t.fresh = false, msg.seq
	}
	if msg.lanes != nil {
		t.wantLanes = *msg.lanes
	}
	t.modeRead = true
	if msg.project != "" {
		t.project = msg.project
	}
	if msg.err != nil && msg.cfg == nil {
		t.err = msg.err.Error()
		t.boards, t.cfg, t.views, t.cards = msg.boards, nil, nil, nil
		m.buildJiraLanes()
		m.renderJira()
		return m, nil
	}
	keep := m.selectedJiraKey()
	t.boards, t.board, t.cfg, t.views, t.viewIdx = msg.boards, msg.board, msg.cfg, msg.views, msg.viewIdx
	t.quick, t.quickOn, t.assignee = withLocalQuick(msg.quick, m.opts.quick), msg.quickOn, msg.assignee
	if msg.statusNames != nil {
		t.statusNames = msg.statusNames
	}
	m.installJiraCards(msg.cards, msg.total, msg.err, keep)
	if msg.cached || msg.err != nil {
		return m, nil
	}
	return m, m.runRules(msg.cards)
}

func (m Model) handleJiraCards(msg jiraCardsMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if msg.seq != t.seq || (msg.cached && t.fresh == msg.seq) {
		return m, nil
	}
	if !msg.cached {
		t.loading, t.fresh = false, msg.seq
	}
	keep := m.selectedJiraKey()
	t.viewIdx = msg.viewIdx
	m.installJiraCards(msg.cards, msg.total, msg.err, keep)
	if msg.cached || msg.err != nil {
		return m, nil
	}
	return m, m.runRules(msg.cards)
}

// installJiraCards shows a fetched card list, keeping the selection on the
// card with key keep when it is still there.
func (m *Model) installJiraCards(cards []jira.Card, total int, err error, keep string) {
	t := m.jiraTab
	if err != nil {
		t.err = err.Error()
	}
	t.cards, t.total = cards, total
	t.fetched = time.Now()
	if t.people == nil {
		t.people = map[string]string{}
	}
	for _, c := range cards {
		if c.AssigneeID != "" {
			t.people[c.AssigneeID] = c.Assignee
		}
	}
	m.buildJiraLanes()
	m.selectJiraKey(keep)
	m.renderJira()
}

// buildJiraLanes sorts the cards into the board's columns (dropping cards
// whose status no column shows) and derives the list order: column by column
// on a board, rank order on a list-only view.
func (m *Model) buildJiraLanes() {
	t := m.jiraTab
	t.lanes = nil
	t.order = t.order[:0]
	t.rows = nil
	v, ok := m.jiraCurrentView()
	if t.cfg == nil || !ok {
		return
	}
	skip := -1
	if v.kind == jiraViewBoard {
		skip = kanbanBacklog(t.cfg)
	}
	q := t.jiraSearchQuery()
	col := map[string]int{}
	for i, c := range t.cfg.Columns {
		if i == skip {
			continue
		}
		for _, id := range c.StatusIDs {
			col[id] = len(t.lanes)
		}
		t.lanes = append(t.lanes, jiraLane{name: c.Name, statusIDs: c.StatusIDs, max: c.Max})
	}
	for i, cd := range t.cards {
		if !jiraCardMatches(cd, q) {
			continue
		}
		if l, ok := col[cd.StatusID]; ok {
			t.lanes[l].cards = append(t.lanes[l].cards, i)
		}
	}
	if v.lanes {
		for _, l := range t.lanes {
			t.order = append(t.order, l.cards...)
		}
	} else {
		for i, cd := range t.cards {
			if jiraCardMatches(cd, q) {
				t.order = append(t.order, i) // a planning list shows every card
			}
		}
	}
	t.sort.apply(t.order, t.cards)
	if len(t.laneTop) != len(t.lanes) {
		t.laneTop = make([]int, len(t.lanes))
	}
	m.clampJiraCursor()
}

func (m *Model) clampJiraCursor() {
	t := m.jiraTab
	t.idx = min(max(t.idx, 0), max(len(t.order)-1, 0))
	t.lane = min(max(t.lane, 0), max(len(t.lanes)-1, 0))
	if t.lane < len(t.lanes) {
		t.row = min(max(t.row, 0), max(len(t.lanes[t.lane].cards)-1, 0))
	} else {
		t.row = 0
	}
}

// selectedJiraCard is the card under the cursor of the current mode.
func (m *Model) selectedJiraCard() (jira.Card, bool) {
	t := m.jiraTab
	if m.jiraShowsLanes() {
		if t.lane < len(t.lanes) && t.row < len(t.lanes[t.lane].cards) {
			return t.cards[t.lanes[t.lane].cards[t.row]], true
		}
		return jira.Card{}, false
	}
	if t.idx < len(t.order) {
		return t.cards[t.order[t.idx]], true
	}
	return jira.Card{}, false
}

func (m *Model) selectedJiraKey() string {
	c, _ := m.selectedJiraCard()
	return c.Key
}

// selectJiraKey puts both cursors on the card with key, when present.
func (m *Model) selectJiraKey(key string) {
	if key == "" {
		return
	}
	t := m.jiraTab
	for i, ci := range t.order {
		if t.cards[ci].Key == key {
			t.idx = i
		}
	}
	for l, lane := range t.lanes {
		for r, ci := range lane.cards {
			if t.cards[ci].Key == key {
				t.lane, t.row = l, r
			}
		}
	}
}

func (m Model) handleJiraKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if key.Matches(msg, m.keys.Palette) {
		m.openPalette()
		return m, nil
	}
	if t.roadmap != nil {
		return m.handleRoadmapKey(msg)
	}
	lanes := m.jiraShowsLanes()
	switch {
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.InputUp):
		m.moveJiraCursor(-1)
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.InputDown):
		m.moveJiraCursor(1)
	case lanes && key.Matches(msg, m.keys.Left):
		m.moveJiraLane(-1)
	case lanes && key.Matches(msg, m.keys.Right):
		m.moveJiraLane(1)
	case lanes && key.Matches(msg, m.keys.MoveCardLeft):
		return m, m.moveJiraCardBy(-1)
	case lanes && key.Matches(msg, m.keys.MoveCardRight):
		return m, m.moveJiraCardBy(1)
	case key.Matches(msg, m.keys.Home):
		t.idx, t.row = 0, 0
		m.renderJira()
	case key.Matches(msg, m.keys.End):
		t.idx, t.row = len(t.order), 1<<30
		m.clampJiraCursor()
		m.renderJira()
	case key.Matches(msg, m.keys.PageUp):
		m.moveJiraCursor(-max(t.view.Height()/2, 1))
	case key.Matches(msg, m.keys.PageDown):
		m.moveJiraCursor(max(t.view.Height()/2, 1))
	case key.Matches(msg, m.keys.OpenChannel), key.Matches(msg, m.keys.OpenRef):
		return m.openJiraCard()
	case key.Matches(msg, m.keys.OpenAttach):
		if c, ok := m.selectedJiraCard(); ok {
			url := m.jiraClient.BrowseURL(c.Key)
			m.status = "opening " + url + "…"
			return m, m.openOpenable(openable{name: c.Key, url: url})
		}
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadJiraBoard(t.project, m.jiraBoardID(), m.jiraViewName(), false)
	case key.Matches(msg, m.keys.Project):
		return m, m.openJiraProjectPicker()
	case key.Matches(msg, m.keys.Board):
		m.openJiraBoardPicker()
	case key.Matches(msg, m.keys.MoveSprint):
		m.openJiraSprintPicker()
	case key.Matches(msg, m.keys.NextView):
		return m, m.cycleJiraView(1)
	case key.Matches(msg, m.keys.PrevView):
		return m, m.cycleJiraView(-1)
	case key.Matches(msg, m.keys.ToggleMode):
		return m, m.toggleJiraMode()
	case key.Matches(msg, m.keys.Roadmap):
		return m, m.openRoadmap()
	case key.Matches(msg, m.keys.Mark):
		m.toggleJiraMark()
	case key.Matches(msg, m.keys.Bulk):
		m.openBulkMenu()
	case key.Matches(msg, m.keys.Assignee):
		m.openJiraAssigneeFilter()
	case key.Matches(msg, m.keys.Mine):
		if t.cfg == nil {
			break
		}
		if t.assignee.id == "me" {
			return m, m.setJiraAssignee("", "")
		}
		return m, m.setJiraAssignee("me", "Me")
	case key.Matches(msg, m.keys.ClearFilters):
		return m, m.clearJiraFilters()
	case key.Matches(msg, m.keys.Search):
		m.startJiraSearch()
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	case key.Matches(msg, m.keys.Sort):
		if lanes {
			m.status = "sort applies to the list (" + helpKey(m.keys.ToggleMode) + ")"
			break
		}
		keep := m.selectedJiraKey()
		t.sort = (t.sort + 1) % jiraSortCount
		m.buildJiraLanes()
		m.selectJiraKey(keep)
		m.renderJira()
		m.status = "sorted by " + t.sort.String()
	case key.Matches(msg, m.keys.Goto):
		m.openJiraGoto()
	case key.Matches(msg, m.keys.Create):
		return m, m.openJiraCreate()
	case key.Matches(msg, m.keys.CopyKey), key.Matches(msg, m.keys.CopyURL):
		return m, m.copyJira(m.selectedJiraKey(), key.Matches(msg, m.keys.CopyURL))
	case msg.String() == "esc" && t.jiraSearchQuery() != "":
		m.clearJiraSearch()
	case msg.String() == "esc" && len(t.marked) > 0:
		m.clearJiraMarks()
		m.status = "marks cleared"
	case len(msg.String()) == 1 && msg.String() >= "1" && msg.String() <= "9":
		return m, m.toggleJiraQuick(int(msg.String()[0] - '1'))
	case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.ShiftTab):
		if m.refOpen {
			m.focus = focusRef
			m.renderJira()
		}
	}
	return m, nil
}

// moveJiraCursor moves down (or up) the list, or the current lane.
func (m *Model) moveJiraCursor(delta int) {
	t := m.jiraTab
	if m.jiraShowsLanes() {
		t.row += delta
	} else {
		t.idx += delta
	}
	m.clampJiraCursor()
	m.renderJira()
}

// moveJiraLane moves the cursor to the next lane (or previous), keeping its
// height as near as the lane allows.
func (m *Model) moveJiraLane(delta int) {
	m.jiraTab.lane += delta
	m.clampJiraCursor()
	m.renderJira()
}

func (m *Model) cycleJiraView(delta int) tea.Cmd {
	t := m.jiraTab
	n := len(t.views)
	if n < 2 {
		return nil
	}
	idx := ((t.viewIdx+delta)%n + n) % n
	return m.loadJiraCards(idx, true)
}

// toggleJiraMode flips lanes and list, remembering the choice. A list-only
// view stays a list.
func (m *Model) toggleJiraMode() tea.Cmd {
	t := m.jiraTab
	if v, ok := m.jiraCurrentView(); ok && !v.lanes {
		m.status = v.name + " is a list"
		return nil
	}
	keep := m.selectedJiraKey()
	t.wantLanes = !t.wantLanes
	m.selectJiraKey(keep)
	m.renderJira()
	mode := "list"
	if t.wantLanes {
		mode = "lanes"
	}
	st := m.store
	return func() tea.Msg {
		_ = st.SetMeta(jiraMetaPrefix+"mode", mode)
		return nil
	}
}

// openJiraCard shows the selected issue in the reference panel.
func (m Model) openJiraCard() (tea.Model, tea.Cmd) {
	c, ok := m.selectedJiraCard()
	if !ok {
		return m, nil
	}
	return m.openJiraKey(c.Key)
}

// openJiraKey shows the issue key in the reference panel, remembering the
// issue it replaces for backspace.
func (m Model) openJiraKey(key string) (tea.Model, tea.Cmd) {
	if r := m.currentRef(); r != nil && r.jiraKey != key {
		m.refBack = append(m.refBack, r.jiraKey)
	}
	return m.showJiraKey(key)
}

// showJiraKey shows key in the panel without touching the history.
func (m Model) showJiraKey(key string) (tea.Model, tea.Cmd) {
	if _, ok := m.jiraTab.highlights[key]; ok {
		delete(m.jiraTab.highlights, key)
		m.jiraTab.rows = nil
	}
	refs := []reference{{kind: refJira, jiraKey: key}}
	m.refOpen = true
	m.refs = refs
	m.refIdx = 0
	m.focus = focusRef
	m.status = m.refStatusHint(refs[0], 1)
	m.resize()
	cmd := m.loadCurrentRef()
	return m, cmd
}

// moveJiraCardBy moves the selected card delta lanes over. A lane of several
// statuses asks which one first.
func (m *Model) moveJiraCardBy(delta int) tea.Cmd {
	t := m.jiraTab
	c, ok := m.selectedJiraCard()
	to := t.lane + delta
	if !ok || to < 0 || to >= len(t.lanes) {
		return nil
	}
	if len(t.lanes[to].statusIDs) > 1 {
		m.openJiraLaneStatusPicker(c, to)
		return nil
	}
	return m.moveJiraCard(c.Key, to, "")
}

// jiraStatusName names a status id, falling back to the id.
func (m *Model) jiraStatusName(id string) string {
	if n := m.jiraTab.statusNames[id]; n != "" {
		return n
	}
	return id
}

// openJiraLaneStatusPicker asks which of lane to's statuses card goes to.
func (m *Model) openJiraLaneStatusPicker(c jira.Card, to int) {
	t := m.jiraTab
	lane := t.lanes[to]
	m.startJiraPicker(jiraPickLaneStatus, c.Key+" → "+lane.name, false)
	items := make([]jiraPickerItem, len(lane.statusIDs))
	for i, id := range lane.statusIDs {
		items[i] = jiraPickerItem{id: id, label: m.jiraStatusName(id), current: id == c.StatusID}
	}
	m.setJiraPickerItems(items)
	t.pendingMove.key, t.pendingMove.lane = c.Key, to
}

// pickJiraLaneStatus moves the card waiting on the status picker.
func (m *Model) pickJiraLaneStatus(statusID string) tea.Cmd {
	pm := m.jiraTab.pendingMove
	return m.moveJiraCard(pm.key, pm.lane, statusID)
}

// moveJiraCard moves the card to lane to: to status statusID, or with ""
// to whichever of the lane's statuses the workflow reaches first. The card
// moves on screen at once, the cursor with it; the move itself may need the
// transition form (jira_transition.go), and the board refetches once Jira
// answers.
func (m *Model) moveJiraCard(key string, to int, statusID string) tea.Cmd {
	t := m.jiraTab
	if to < 0 || to >= len(t.lanes) {
		return nil
	}
	lane := t.lanes[to]
	ci := slices.IndexFunc(t.cards, func(c jira.Card) bool { return c.Key == key })
	if ci < 0 || len(lane.statusIDs) == 0 {
		return nil
	}
	cur := t.cards[ci].StatusID
	if statusID == cur || (statusID == "" && slices.Contains(lane.statusIDs, cur)) {
		return nil
	}
	target, name := lane.statusIDs[0], lane.name
	want := func(tm jira.TransitionMeta) bool { return slices.Contains(lane.statusIDs, tm.ToID) }
	if statusID != "" {
		target, name = statusID, m.jiraStatusName(statusID)
		want = func(tm jira.TransitionMeta) bool { return tm.ToID == statusID }
	}
	t.cards[ci].StatusID = target
	t.cards[ci].Status = name
	m.buildJiraLanes()
	m.selectJiraKey(key)
	m.renderJira()
	m.status = fmt.Sprintf("moving %s → %s…", key, name)
	return m.prepareJiraMove(key, name, jiraFromBoard, want)
}

func (m Model) handleJiraMoved(msg jiraMovedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("%s: move failed: %v", msg.key, msg.err)
	} else {
		m.status = fmt.Sprintf("%s → %s", msg.key, msg.lane)
	}
	return m, m.loadJiraCards(m.jiraTab.viewIdx, false)
}

// refreshJiraAfterEdit refetches the board after the panel changed an issue on
// it, so a new status or assignee shows on its card.
func (m *Model) refreshJiraAfterEdit() tea.Cmd {
	if m.jiraTab.cfg == nil {
		return nil
	}
	return m.loadJiraCards(m.jiraTab.viewIdx, false)
}

// openJiraProjectPicker lists the projects, configured ones first, in a
// filterable picker.
func (m *Model) openJiraProjectPicker() tea.Cmd {
	gen := m.startJiraPicker(jiraPickProject, "Project", true)
	t := m.jiraTab
	cur, configured := t.project, m.jiraProjects
	if t.projects != nil {
		m.setJiraPickerItems(jiraProjectItems(t.projects, configured, cur))
		return nil
	}
	seq, c, ctx := m.jiraPicker.fetchSeq, m.jiraClient, m.ctx
	return func() tea.Msg {
		ps, err := c.ListProjects(ctx)
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickProject, projects: ps,
			items: jiraProjectItems(ps, configured, cur), err: err}
	}
}

func jiraProjectItems(ps []jira.Project, configured []string, cur string) []jiraPickerItem {
	var first, rest []jiraPickerItem
	for _, p := range ps {
		it := jiraPickerItem{id: p.Key, label: p.Key + "  " + p.Name, current: p.Key == cur}
		if slices.Contains(configured, p.Key) {
			first = append(first, it)
		} else {
			rest = append(rest, it)
		}
	}
	return append(first, rest...)
}

// openJiraBoardPicker lists the project's boards.
func (m *Model) openJiraBoardPicker() {
	t := m.jiraTab
	if len(t.boards) == 0 {
		return
	}
	m.startJiraPicker(jiraPickBoard, "Board — "+t.project, false)
	items := make([]jiraPickerItem, len(t.boards))
	for i, b := range t.boards {
		items[i] = jiraPickerItem{id: strconv.Itoa(b.ID), label: b.Name + "  " + b.Type, current: i == t.board}
	}
	m.setJiraPickerItems(items)
}

// pickJiraBoard loads a board picked from the project or board picker.
func (m *Model) pickJiraBoard(kind jiraPickerKind, id string) tea.Cmd {
	t := m.jiraTab
	if kind == jiraPickProject {
		if id == t.project {
			return nil
		}
		t.project = id
		t.boards, t.cfg, t.views, t.cards = nil, nil, nil, nil
		t.quick, t.quickOn, t.people = nil, nil, nil
		t.idx, t.lane, t.row, t.firstLane = 0, 0, 0, 0
		m.buildJiraLanes()
		return m.loadJiraBoard(id, 0, "", true)
	}
	board, _ := strconv.Atoi(id)
	if board == m.jiraBoardID() {
		return nil
	}
	t.idx, t.lane, t.row, t.firstLane = 0, 0, 0, 0
	t.quick, t.quickOn, t.people = nil, nil, nil
	return m.loadJiraBoard(t.project, board, "", true)
}

// openJiraAssigneeFilter offers everyone, you, unassigned, and the people
// seen on the board, in a filterable picker.
func (m *Model) openJiraAssigneeFilter() {
	t := m.jiraTab
	if t.cfg == nil {
		return
	}
	m.startJiraPicker(jiraPickBoardAssignee, "Assignee", true)
	items := []jiraPickerItem{{id: "", label: "Everyone"}, {id: "me", label: "Me"}, {id: "none", label: "Unassigned"}}
	var people []jiraPickerItem
	for id, name := range t.people {
		people = append(people, jiraPickerItem{id: id, label: name})
	}
	slices.SortFunc(people, func(a, b jiraPickerItem) int { return strings.Compare(a.label, b.label) })
	items = append(items, people...)
	for i := range items {
		items[i].current = items[i].id == t.assignee.id
	}
	m.setJiraPickerItems(items)
}

// setJiraAssignee applies an assignee filter picked from the picker and
// remembers it.
func (m *Model) setJiraAssignee(id, label string) tea.Cmd {
	t := m.jiraTab
	if id == "" {
		label = ""
	}
	t.assignee = jiraAssignee{id: id, label: label}
	st := m.store
	save := func() tea.Msg {
		_ = st.SetMeta(jiraMetaPrefix+"assignee", id+"\t"+label)
		return nil
	}
	return tea.Batch(m.loadJiraCards(t.viewIdx, true), save)
}

// toggleJiraQuick flips the board's quick filter i (0-based) and remembers
// the board's set.
func (m *Model) toggleJiraQuick(i int) tea.Cmd {
	t := m.jiraTab
	if i < 0 || i >= len(t.quick) {
		return nil
	}
	if t.quickOn == nil {
		t.quickOn = map[int]bool{}
	}
	id := t.quick[i].ID
	t.quickOn[id] = !t.quickOn[id]
	return tea.Batch(m.loadJiraCards(t.viewIdx, true), m.saveJiraQuick())
}

// clearJiraFilters turns every filter off.
func (m *Model) clearJiraFilters() tea.Cmd {
	t := m.jiraTab
	if t.assignee.id == "" && len(t.quickOn) == 0 {
		return nil
	}
	t.quickOn = map[int]bool{}
	return tea.Batch(m.setJiraAssignee("", ""), m.saveJiraQuick())
}

func (m *Model) saveJiraQuick() tea.Cmd {
	t := m.jiraTab
	var ids []string
	for _, q := range t.quick {
		if t.quickOn[q.ID] {
			ids = append(ids, strconv.Itoa(q.ID))
		}
	}
	st, key := m.store, jiraMetaPrefix+"quick:"+strconv.Itoa(m.jiraBoardID())
	return func() tea.Msg {
		_ = st.SetMeta(key, strings.Join(ids, ","))
		return nil
	}
}

// jiraFiltered reports whether any filter is on.
func (t *jiraTabState) jiraFiltered() bool {
	return jiraFilterJQL(t.assignee, t.quick, t.quickOn) != ""
}

// jiraListWidth is the board pane's outer width: the whole body, or what the
// reference panel leaves of it.
func (m *Model) jiraListWidth(width int) (list, ref int) {
	if !m.refOpen {
		return width, 0
	}
	ref = splitRightPane(width, m.opts.panelPct)
	return width - ref, ref
}

func (m *Model) sizeJiraView(width, height int) {
	list, _ := m.jiraListWidth(width)
	m.jiraTab.view.SetWidth(max(list-2, 10))
	m.jiraTab.view.SetHeight(max(height-4, 1)) // title row, rule, views, filters
}

var jiraLaneStyle = lipgloss.NewStyle().Bold(true)

// Themed board styles, set by applyTheme. jiraOverStyle marks a lane past
// its WIP limit.
var jiraKeyStyle, jiraDimStyle, jiraOverStyle, jiraDropStyle, jiraViewActive, jiraGhostStyle lipgloss.Style

// jiraTypeIcon is a nerd-font glyph per issue type, like the GitLab tab's.
func jiraTypeIcon(t string) string {
	st := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	switch strings.ToLower(t) {
	case "bug":
		return st(curTheme["type_bug"]).Render("")
	case "story":
		return st(curTheme["type_story"]).Render("")
	case "epic":
		return st(curTheme["type_epic"]).Render("")
	case "sub-task", "subtask":
		return st(curTheme["type_subtask"]).Render("")
	}
	return st(curTheme["type_other"]).Render("")
}

// jiraPriorityMark marks a card's priority, "" for medium or none: the
// default needs no ink.
func jiraPriorityMark(p string) string {
	st := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	switch strings.ToLower(p) {
	case "highest", "blocker", "critical":
		return st(curTheme["priority_highest"]).Render("⇈")
	case "high", "major":
		return st(curTheme["priority_high"]).Render("↑")
	case "low", "minor":
		return st(curTheme["priority_low"]).Render("↓")
	case "lowest", "trivial":
		return st(curTheme["priority_lowest"]).Render("⇊")
	}
	return ""
}

// renderJira rebuilds the body: the list viewport's content, or the cached
// lane render, with the cursor scrolled into view.
func (m *Model) renderJira() {
	t := m.jiraTab
	w, h := t.view.Width(), t.view.Height()
	var msg string
	switch {
	case t.err != "":
		msg = refErrStyle.Render(t.err)
	case t.cfg == nil:
		msg = refDimStyle.Render("loading…")
	case len(t.order) == 0 && t.jiraSearchQuery() != "":
		msg = refDimStyle.Render("no issues match /" + t.search.Value() + " (esc clears)")
	case len(t.cards) == 0 && t.jiraFiltered():
		msg = refDimStyle.Render("no issues match the filters (0 clears them)")
	case len(t.order) == 0:
		msg = refDimStyle.Render("no issues")
	}
	if msg != "" {
		t.view.SetContent(msg)
		t.lanesOut = msg
		return
	}
	if m.jiraShowsLanes() {
		t.lanesOut = m.renderJiraLanes(w, h)
		return
	}
	keyW, stW := 0, 0
	for _, ci := range t.order {
		keyW = max(keyW, len(t.cards[ci].Key))
		stW = max(stW, lipgloss.Width(t.cards[ci].Status))
	}
	stW = min(stW, 20)
	if t.rowsFor != [3]int{w, keyW, stW} || len(t.rows) != len(t.order) {
		t.rows = make([]string, len(t.order))
		for i, ci := range t.order {
			t.rows[i] = m.jiraListRow(t.cards[ci], false, w, keyW, stW)
		}
		t.rowsFor = [3]int{w, keyW, stW}
	}
	lines := t.rows
	if t.idx < len(t.order) {
		lines = slices.Clone(t.rows)
		lines[t.idx] = m.jiraListRow(t.cards[t.order[t.idx]], true, w, keyW, stW)
	}
	t.view.SetContentLinesWidth(lines, w)
	top := t.view.YOffset()
	switch r := t.idx; {
	case r < top:
		t.view.SetYOffset(r)
	case r >= top+h:
		t.view.SetYOffset(r - h + 1)
	}
}

func (m *Model) jiraListRow(c jira.Card, selected bool, width, keyW, stW int) string {
	status := ansi.Truncate(c.Status, stW, "…")
	status += strings.Repeat(" ", max(stW-lipgloss.Width(status), 0))
	pts := fmt.Sprintf("%3s", c.Points)
	f := m.opts.fields
	title := c.Summary
	if f.assignee && c.Assignee != "" {
		title += jiraDimStyle.Render(" · " + c.Assignee)
	}
	if f.parent && c.ParentSummary != "" {
		title += jiraDimStyle.Render(" · ⌃ " + c.ParentSummary)
	}
	row := "  "
	if hl := m.jiraHighlight(c.Key); hl != "" {
		row = hl + " "
	}
	row += jiraKeyStyle.Render(fmt.Sprintf("%-*s", keyW, c.Key)) + "  "
	if f.typ {
		row += jiraTypeIcon(c.Type) + " "
	}
	if f.priority {
		pm := jiraPriorityMark(c.Priority)
		if pm == "" {
			pm = " "
		}
		row += pm + " "
	}
	if f.status {
		row += jiraDimStyle.Render(status) + "  "
	}
	if f.points {
		row += jiraDimStyle.Render(pts) + "  "
	}
	row += title
	row = ansi.Truncate(row, width-1, "…")
	return m.jiraSelect(row, selected, width)
}

// jiraHighlight is the mark of a card a rule highlighted, "" for none.
func (m *Model) jiraHighlight(key string) string {
	if mk := m.jiraMark(key); mk != "" {
		return mk
	}
	c, ok := m.jiraTab.highlights[key]
	if !ok {
		return ""
	}
	if c == "" {
		c = curTheme["highlight"]
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
}

// jiraSelect paints a selected row across width: bright while the board has
// the keys, quiet while the panel does.
func (m *Model) jiraSelect(row string, selected bool, width int) string {
	if !selected {
		return row
	}
	row = keepBG(row)
	if pad := width - visualWidth(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	if m.focus == focusJira {
		return selectedRow.Render(row)
	}
	return diffTreeSelStyle.Render(row)
}

// jiraLanePoints sums the story points of a lane's cards; false when none
// is estimated.
func jiraLanePoints(cards []jira.Card, lane []int) (string, bool) {
	sum, any := 0.0, false
	for _, ci := range lane {
		if f, err := strconv.ParseFloat(cards[ci].Points, 64); err == nil {
			sum, any = sum+f, true
		}
	}
	return strconv.FormatFloat(math.Round(sum*100)/100, 'f', -1, 64), any
}

// jiraLaneLayout is how many lanes fit in width, and how wide each is.
func jiraLaneLayout(width, n int) (visible, laneW int) {
	if n == 0 {
		return 0, width
	}
	visible = min(n, max(width/jiraLaneMinW, 1))
	return visible, width / visible
}

// jiraCardLines is a card's three lines: key, type and points; summary;
// assignee. styled false leaves them plain, for the drag ghost.
func jiraCardLines(c jira.Card, styled bool, f cardFields) []string {
	key, pts, who := c.Key, "", ""
	if f.points && c.Points != "" {
		pts = " " + c.Points
	}
	if f.assignee {
		who = c.Assignee
		if who == "" {
			who = "unassigned"
		}
	}
	if f.parent && c.ParentSummary != "" {
		if who != "" {
			who += " · "
		}
		who += "⌃ " + c.ParentSummary
	}
	if !styled {
		return []string{key + pts, c.Summary, who}
	}
	head := jiraKeyStyle.Render(key)
	if f.typ {
		head += " " + jiraTypeIcon(c.Type)
	}
	if pm := jiraPriorityMark(c.Priority); f.priority && pm != "" {
		head += " " + pm
	}
	return []string{head + jiraDimStyle.Render(pts), c.Summary, jiraDimStyle.Render(who)}
}

// jiraDropZones splits a lane body into one drop zone per status while a
// card is dragged over it, the zone under the pointer lit and the card's own
// status marked, as Jira's board does.
func (m *Model) jiraDropZones(lane jiraLane, ghost, width, height int) []string {
	t := m.jiraTab
	cur := ""
	if ghost >= 0 {
		cur = t.cards[ghost].StatusID
	}
	zh := jiraZoneH(height+1, len(lane.statusIDs))
	var out []string
	for i, id := range lane.statusIDs {
		name := m.jiraStatusName(id)
		if id == cur {
			name += " (now)"
		}
		style := jiraGhostStyle
		if i == t.drag.zone {
			style = jiraDropStyle
		}
		for r := 0; r < zh; r++ {
			var line string
			switch {
			case r == zh-1 && zh > 1:
				line = "" // a gap between zones
				out = append(out, line)
				continue
			case r == (zh-1)/2:
				line = " " + ansi.Truncate(name, width-2, "…")
			}
			line += strings.Repeat(" ", max(width-lipgloss.Width(line), 0))
			if i != t.drag.zone {
				line = "┊" + line[min(len(line), 1):]
			}
			out = append(out, style.Render(line))
		}
	}
	return out
}

// renderJiraLanes draws the visible lanes side by side: a header with the
// column name and count, then the cards, each lane scrolled to keep the cursor
// on screen. A lane a card is being dragged over lights its header.
func (m *Model) renderJiraLanes(width, height int) string {
	t := m.jiraTab
	visible, laneW := jiraLaneLayout(width, len(t.lanes))
	t.laneW = laneW
	if t.lane < t.firstLane {
		t.firstLane = t.lane
	}
	if t.lane >= t.firstLane+visible {
		t.firstLane = t.lane - visible + 1
	}
	t.firstLane = min(max(t.firstLane, 0), max(len(t.lanes)-visible, 0))
	slots := max((height-1)/jiraCardSlot, 1)
	if t.lane < len(t.laneTop) {
		top := &t.laneTop[t.lane]
		if t.row < *top {
			*top = t.row
		}
		if t.row >= *top+slots {
			*top = t.row - slots + 1
		}
	}
	ghost := -1 // the dragged card, as an index into cards
	if t.drag.active {
		ghost = slices.IndexFunc(t.cards, func(c jira.Card) bool { return c.Key == t.drag.key })
	}
	cols := make([][]string, 0, visible)
	for l := t.firstLane; l < t.firstLane+visible && l < len(t.lanes); l++ {
		inner := laneW - 1
		lane := t.lanes[l]
		count := strconv.Itoa(len(lane.cards))
		if lane.max > 0 {
			count += "/" + strconv.Itoa(lane.max)
		}
		if pts, ok := jiraLanePoints(t.cards, lane.cards); ok && m.opts.fields.points {
			count += " · " + pts + "p"
		}
		head := ansi.Truncate(lane.name+" "+count, inner, "…")
		// Filtered counts undercount the column, so only a full board judges it.
		over := lane.max > 0 && len(lane.cards) > lane.max && !t.jiraFiltered() && t.jiraSearchQuery() == ""
		switch {
		case over && !(t.drag.active && l == t.drag.over):
			head = jiraOverStyle.Underline(l == t.lane).Render(head)
		case t.drag.active && l == t.drag.over:
			head = jiraDropStyle.Render(head + strings.Repeat(" ", max(inner-lipgloss.Width(head), 0)))
		case l == t.lane:
			head = jiraViewActive.Render(head)
		default:
			head = jiraLaneStyle.Render(head)
		}
		col := []string{head}
		if t.drag.active && l == t.drag.over && len(lane.statusIDs) > 1 {
			cols = append(cols, append(col, m.jiraDropZones(lane, ghost, inner, height-1)...))
			continue
		}
		// While a card is dragged over this lane, its ghost sits where the card
		// will land: lanes keep the view's rank order, so after every card of
		// the lane ranked above it.
		type slot struct {
			ci    int
			ghost bool
		}
		slots := make([]slot, 0, len(lane.cards)+1)
		for _, ci := range lane.cards {
			slots = append(slots, slot{ci: ci})
		}
		top := 0
		if l < len(t.laneTop) {
			top = min(t.laneTop[l], max(len(lane.cards)-1, 0))
		}
		if g := ghost; g >= 0 && l == t.drag.over && l != t.drag.from {
			at := 0
			for at < len(lane.cards) && lane.cards[at] < g {
				at++
			}
			slots = slices.Insert(slots, at, slot{ci: g, ghost: true})
			top = min(top, at)
			if fit := max((height-1)/jiraCardSlot, 1); at >= top+fit {
				top = at - fit + 1
			}
		}
		for r := top; r < len(slots) && len(col)+jiraCardH <= height; r++ {
			c := t.cards[slots[r].ci]
			if slots[r].ghost || (slots[r].ci == ghost && l == t.drag.from && t.drag.over != l) {
				// The ghost, and the card it left behind: plain text, faint.
				for _, line := range jiraCardLines(c, false, m.opts.fields) {
					col = append(col, jiraGhostStyle.Render(ansi.Truncate("┊ "+line, inner, "…")))
				}
				col = append(col, "")
				continue
			}
			sel := l == t.lane && t.row < len(lane.cards) && lane.cards[t.row] == slots[r].ci
			lines := jiraCardLines(c, true, m.opts.fields)
			if hl := m.jiraHighlight(c.Key); hl != "" {
				lines[0] = hl + " " + lines[0]
			}
			for _, line := range lines {
				col = append(col, m.jiraSelect(ansi.Truncate(line, inner, "…"), sel, inner))
			}
			col = append(col, "")
		}
		cols = append(cols, col)
	}
	sep := jiraDimStyle.Render("│")
	lines := make([]string, height)
	for y := range lines {
		var b strings.Builder
		for i, col := range cols {
			cell := ""
			if y < len(col) {
				cell = col[y]
			}
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", max(laneW-1-lipgloss.Width(cell), 0)))
			if i < len(cols)-1 {
				b.WriteString(sep)
			}
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

// renderJiraPane draws the tab body: the board pane, plus the reference panel
// on the right while one is open.
func (m *Model) renderJiraPane(height, width int) string {
	t := m.jiraTab
	listW, refW := m.jiraListWidth(width)
	innerH := max(height-1, 1)
	boxW := listW
	if refW > 0 {
		boxW++ // its right border is cut below; the panel's left border divides
	}

	title := titleStyle.Render("Jira")
	if t.project != "" {
		title += " " + titleStyle.Render(t.project)
	}
	if t.board < len(t.boards) {
		title += refDimStyle.Render("  " + t.boards[t.board].Name)
	}
	meta := ""
	switch {
	case t.loading:
		meta = "  refreshing…"
	case !t.fetched.IsZero():
		meta = "  updated " + age(t.fetched)
		if t.total > len(t.cards) {
			meta += fmt.Sprintf("  ·  first %d of %d", len(t.cards), t.total)
		}
	}
	k := m.keys
	meta += "  ·  " + helpKey(k.Help) + " help  " + helpKey(k.Project) + " project  " + helpKey(k.Board) + " board  " +
		helpKey(k.PrevView) + " " + helpKey(k.NextView) + " view  " + helpKey(k.ToggleMode) + " lanes/list  " +
		helpKey(k.OpenChannel) + " open  " + helpKey(k.OpenAttach) + " browser  " + helpKey(k.Refresh) + " refresh"
	if m.jiraShowsLanes() {
		meta += "  " + helpKey(k.MoveCardLeft) + "/" + helpKey(k.MoveCardRight) + " move"
	}
	head := ansi.Truncate(title+refDimStyle.Render(meta), max(boxW-2, 1), "…")
	rule := refDimStyle.Render(strings.Repeat("─", max(boxW-2, 1)))

	var views []string
	for i, v := range t.views {
		if i == t.viewIdx {
			views = append(views, jiraViewActive.Render(v.name))
		} else {
			views = append(views, jiraDimStyle.Render(v.name))
		}
	}
	viewLine := strings.Join(views, jiraDimStyle.Render("  │  "))
	if v, ok := m.jiraCurrentView(); ok {
		if s := jiraSprintLine(v, time.Now()); s != "" {
			viewLine += jiraDimStyle.Render("    " + s)
		}
	}
	if n := len(t.lanes); m.jiraShowsLanes() && n > 0 {
		if vis, _ := jiraLaneLayout(t.view.Width(), n); vis < n {
			viewLine += jiraDimStyle.Render(fmt.Sprintf("    lanes %d–%d of %d", t.firstLane+1, t.firstLane+vis, n))
		}
	}
	viewLine = ansi.Truncate(viewLine, max(boxW-2, 1), "…")
	filterLine := ansi.Truncate(m.jiraFilterLine(), max(boxW-2, 1), "…")

	body := t.view.View()
	switch {
	case t.roadmap != nil:
		viewLine = ansi.Truncate(m.roadmapLine(), max(boxW-2, 1), "…")
		filterLine = ""
		body = m.renderRoadmap(t.view.Width(), t.view.Height())
	case m.jiraShowsLanes() || t.cfg == nil || len(t.order) == 0:
		body = t.lanesOut
	}
	rows := []string{head, rule, viewLine, filterLine, body}
	borderColor := dimColor
	if m.focus == focusJira {
		borderColor = focusedColor
	}
	content := strings.Join(rows, "\n")
	box, ok := renderPaneBox(content, boxW, innerH, borderColor)
	if !ok {
		box = lipgloss.NewStyle().Border(border).UnsetBorderTop().
			Width(boxW).Height(innerH).BorderForeground(borderColor).Render(content)
	}
	list := joinRuleRows(box, 1)
	if refW == 0 {
		return list
	}
	lines := strings.Split(list, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, listW, "")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(lines, "\n"), m.renderRefPane(height, refW))
}

// jiraSprintLine is a sprint view's time and goal: "5d left · Ship it".
func jiraSprintLine(v jiraView, now time.Time) string {
	if v.kind != jiraViewSprint {
		return ""
	}
	var parts []string
	days := func(t time.Time) int { return int(math.Ceil(t.Sub(now).Hours() / 24)) }
	switch {
	case !v.start.IsZero() && v.start.After(now):
		parts = append(parts, "starts "+v.start.Local().Format("Jan 2"))
	case !v.end.IsZero() && days(v.end) > 0:
		parts = append(parts, strconv.Itoa(days(v.end))+"d left")
	case !v.end.IsZero():
		parts = append(parts, "ended "+v.end.Local().Format("Jan 2"))
	}
	if g := safeterm.Line(strings.Join(strings.Fields(v.goal), " ")); g != "" {
		parts = append(parts, g)
	}
	return strings.Join(parts, " · ")
}

// jiraFilterLine shows the filters and their keys, the ones that are on lit.
func (m *Model) jiraFilterLine() string {
	t := m.jiraTab
	chip := func(on bool, s string) string {
		if on {
			return jiraViewActive.Render(s)
		}
		return jiraDimStyle.Render(s)
	}
	who := "everyone"
	if t.assignee.id != "" {
		who = t.assignee.label
	}
	line := jiraDimStyle.Render(helpKey(m.keys.Assignee)+" assignee ("+helpKey(m.keys.Mine)+" me): ") + chip(t.assignee.id != "", who)
	for i, q := range t.quick {
		if i == 9 {
			break
		}
		line += "  " + chip(t.quickOn[q.ID], strconv.Itoa(i+1)+" "+q.Name)
	}
	if t.jiraFiltered() {
		line += jiraDimStyle.Render("  ·  " + helpKey(m.keys.ClearFilters) + " clears")
	}
	if t.sort != jiraSortRank && !m.jiraShowsLanes() {
		line += jiraDimStyle.Render("  ·  "+helpKey(m.keys.Sort)+" sort: ") + chip(true, t.sort.String())
	}
	switch {
	case t.searching:
		line = t.search.View() + "  " + line
	case t.jiraSearchQuery() != "":
		line = chip(true, "/"+t.search.Value()) + jiraDimStyle.Render(" esc") + "  " + line
	default:
		line += jiraDimStyle.Render("  ·  " + helpKey(m.keys.Search) + " search")
	}
	return line
}

// hitJira maps a screen cell on the board to a card: idx is the lane (-1 in
// list mode) and line the card's row in it, -1 over no card.
func (m *Model) hitJira(x, y int) hit {
	t := m.jiraTab
	line := y - jiraBodyTop
	if !m.jiraShowsLanes() {
		if line >= 0 && line < t.view.Height() {
			if r := t.view.YOffset() + line; r < len(t.order) {
				return hit{zone: hitJira, idx: -1, line: r}
			}
		}
		return hit{zone: hitJira, idx: -1, line: -1}
	}
	visible, laneW := jiraLaneLayout(t.view.Width(), len(t.lanes))
	col := (x - 1) / max(laneW, 1)
	if x < 1 || col >= visible || t.firstLane+col >= len(t.lanes) {
		return hit{zone: hitJira, idx: -1, line: -1}
	}
	lane := t.firstLane + col
	h := hit{zone: hitJira, idx: lane, line: -1}
	if line >= 1 {
		r := (line-1)/jiraCardSlot + t.laneTop[lane]
		if (line-1)%jiraCardSlot < jiraCardH && r < len(t.lanes[lane].cards) {
			h.line = r
		}
	}
	return h
}

// clickJira selects the clicked card and arms a drag on it; a double-click
// opens it in the panel.
func (m Model) clickJira(h hit, x, y, count int) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	m.focus = focusJira
	if h.line < 0 {
		if h.idx >= 0 {
			t.lane = h.idx
			m.clampJiraCursor()
		}
		m.renderJira()
		return m, nil
	}
	if h.idx < 0 {
		t.idx = h.line
	} else {
		t.lane, t.row = h.idx, h.line
		if c, ok := m.selectedJiraCard(); ok && count == 1 {
			t.drag = jiraDrag{key: c.Key, x: x, y: y, from: h.idx, over: h.idx}
		}
	}
	m.renderJira()
	if count == 2 {
		return m.openJiraCard()
	}
	return m, nil
}

// dragJira follows a dragged card: the lane under the pointer lights up, and
// the pointer at the pane's edge scrolls hidden lanes into view.
func (m Model) dragJira(x, y int) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if !t.drag.active {
		if x-t.drag.x < 2 && t.drag.x-x < 2 && y == t.drag.y {
			return m, nil
		}
		t.drag.active = true
	}
	visible, _ := jiraLaneLayout(t.view.Width(), len(t.lanes))
	listW, _ := m.jiraListWidth(m.width)
	over, scrolled := t.drag.over, true
	switch {
	case x >= listW-2 && t.firstLane+visible < len(t.lanes):
		t.firstLane++
		over = t.firstLane + visible - 1
		t.lane = over // renderJiraLanes keeps the cursor lane on screen
	case x <= 1 && t.firstLane > 0:
		t.firstLane--
		over = t.firstLane
		t.lane = over
	default:
		scrolled = false
		if h := m.hitJira(x, y); h.idx >= 0 {
			over = h.idx
		}
	}
	zone := 0
	if over >= 0 && over < len(t.lanes) {
		if n := len(t.lanes[over].statusIDs); n > 1 {
			zone = min(max((y-jiraBodyTop-1)/jiraZoneH(t.view.Height(), n), 0), n-1)
		}
	}
	if over != t.drag.over || zone != t.drag.zone || scrolled {
		t.drag.over, t.drag.zone = over, zone
		m.renderJira()
	}
	return m, nil
}

// dropJira ends a drag, moving the card when it landed on another lane, or on
// another status of a lane of several.
func (m Model) dropJira() (tea.Model, tea.Cmd) {
	t := m.jiraTab
	d := t.drag
	t.drag = jiraDrag{}
	if !d.active {
		return m, nil
	}
	status := ""
	if d.over >= 0 && d.over < len(t.lanes) {
		if ids := t.lanes[d.over].statusIDs; len(ids) > 1 && d.zone >= 0 && d.zone < len(ids) {
			status = ids[d.zone]
		}
	}
	if d.over == d.from && status == "" {
		m.renderJira()
		return m, nil
	}
	if cmd := m.moveJiraCard(d.key, d.over, status); cmd != nil {
		return m, cmd
	}
	m.selectJiraKey(d.key)
	m.renderJira()
	return m, nil
}

// jiraZoneH is the height of one status drop zone in a lane body of height
// rows split n ways.
func jiraZoneH(height, n int) int {
	return max((height-1)/max(n, 1), 1)
}

func (m *Model) jiraDragging() bool {
	return m.jiraTab.drag.key != ""
}

// jiraAutoRefreshMsg is the auto-refresh tick.
type jiraAutoRefreshMsg struct{}

// jiraAutoRefreshTick arms the next auto-refresh, none when it is off.
func (m *Model) jiraAutoRefreshTick() tea.Cmd {
	if m.opts.autoRefresh <= 0 {
		return nil
	}
	return tea.Tick(m.opts.autoRefresh, func(time.Time) tea.Msg { return jiraAutoRefreshMsg{} })
}

// handleJiraAutoRefresh refetches the view unless the user is mid-action or
// the board is fresh; the next tick is always armed.
func (m Model) handleJiraAutoRefresh() (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if m.modalOpen() || t.loading || t.searching || m.jiraDragging() || t.cfg == nil || time.Since(t.fetched) < m.opts.staleAfter {
		return m, m.jiraAutoRefreshTick()
	}
	return m, tea.Batch(m.loadJiraCards(t.viewIdx, false), m.jiraAutoRefreshTick())
}
