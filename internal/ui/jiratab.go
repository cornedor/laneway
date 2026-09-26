package ui

import (
	"context"
	"fmt"
	"hash/fnv"
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
	// jiraViewFilter is a starred Jira filter: its own search, any board.
	jiraViewFilter
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
	// doneDays is how long a kanban board shows done work (0: the default).
	doneDays int
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
// andOrderedJQL is andJQL for a query that may end in ORDER BY, which
// stays last.
func andOrderedJQL(q, b string) string {
	where, order := q, ""
	if i := strings.LastIndex(strings.ToUpper(q), "ORDER BY"); i >= 0 {
		where, order = strings.TrimSpace(q[:i]), " "+q[i:]
	}
	if where == "" {
		return strings.TrimSpace(b + order)
	}
	return andJQL(where, b) + order
}

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
	// band is a card of the swimlane under the pointer, ok when over one.
	band   jira.Card
	bandOK bool
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
	// lastMove is the card last moved and the status it came from, for u;
	// lastBand the card last dropped into another swimlane and its values
	// before. Each is stamped from undoSeq: u reverts the latest stamp.
	lastMove [2]string
	lastBand jiraBandUndo
	moveSeq  int
	undoSeq  int
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
	// lineOf is each order entry's line in the list, which group headers
	// (sorted by assignee or priority) push down.
	lineOf []int

	idx       int   // list cursor, into order
	lane, row int   // lane cursor
	laneTop   []int // per lane, the first card on screen
	firstLane int   // the first lane on screen
	laneW     int   // a lane's width in the last render
	lanesOut  string

	sort jiraSort // the list's order; lanes keep the board's rank
	// swim groups the lanes into swimlanes by assignee, epic or priority (jiraSortRank
	// for none); swimTop is its first line on screen, swimAt each body
	// line's card row per lane (-1 for none), for the mouse.
	swim    jiraSort
	swimTop int
	swimAt  [][]int
	// swimBand is each body line's band, as one of its cards.
	swimBand []jira.Card
	// swimFold are the folded bands, by name, until the swimlanes change.
	swimFold map[string]bool

	// search narrows the cards locally; searching while it has the keyboard.
	search    textinput.Model
	searching bool
	// viewsFirst is the first view the header shows (render sets it, for
	// clicks).
	viewsFirst int
	// fullAt and fullKey are when and for which view and filters the cards
	// were last fetched whole; an idle refresh within ui.full_refresh of it
	// fetches only what changed (loadJiraDelta).
	fullAt  time.Time
	fullKey string
	// roadmap and plan show in place of the cards while non-nil
	// (roadmap.go, planning.go).
	roadmap *roadmapState
	plan    *planState
	charts  *chartsState // charts.go

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
	delta   bool     // only the cards updated since the last fetch
	gone    []string // with delta: loaded cards that left the view
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
	assignee, quickOn, quickBoard, local, localViews := t.assignee, t.quickOn, m.jiraBoardID(), m.opts.quick, append(slices.Clone(m.opts.views), m.savedJQLViews()...)
	withSaved, doneDays := m.opts.savedFilters, m.opts.kanbanDoneDays
	var cached tea.Cmd
	if fromCache {
		cached = jiraBoardFromCache(st, seq, project, boardID, view, configured, readMode)
	}
	return tea.Batch(cached, func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(60*time.Second))
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
		var saved []jira.QuickFilter
		wg.Add(5)
		go func() {
			defer wg.Done()
			if withSaved {
				saved, _ = c.FavouriteFilters(ctx) // views are extras: a failure just leaves them out
			}
		}()
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
			msg.views = append(msg.views, jiraView{kind: jiraViewBoard, name: "Board", lanes: true, doneDays: doneDays})
			if kanbanBacklog(cfg) >= 0 {
				msg.views = append(msg.views, jiraView{kind: jiraViewBacklog, name: "Backlog"})
			}
		}
		msg.views = append(msg.views, localViews...)
		for _, f := range saved {
			msg.views = append(msg.views, jiraView{kind: jiraViewFilter, name: f.Name, jql: f.JQL})
		}
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
// days (two weeks by default) ago.
func jiraKanbanJQL(days int) string {
	if days <= 0 {
		days = defaultKanbanDoneDays
	}
	return fmt.Sprintf("statusCategory != Done OR updated >= -%dd", days)
}

const defaultKanbanDoneDays = 14

func fetchJiraView(ctx context.Context, c *jira.Client, board int, cfg *jira.BoardConfig, v jiraView, filter string) ([]jira.Card, int, error) {
	switch v.kind {
	case jiraViewSprint:
		return c.SprintIssues(ctx, board, v.sprint, filter, cfg.PointsField)
	case jiraViewBacklog:
		return c.BacklogIssues(ctx, board, filter, cfg.PointsField)
	case jiraViewJQL:
		return c.BoardIssues(ctx, board, andJQL(v.jql, filter), cfg.PointsField)
	case jiraViewFilter:
		cards, err := c.SearchCards(ctx, andOrderedJQL(v.jql, filter))
		return cards, len(cards), err
	}
	cards, total, err := c.BoardIssues(ctx, board, andJQL(jiraKanbanJQL(v.doneDays), filter), cfg.PointsField)
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
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(60*time.Second))
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
	t.swim = m.readJiraSwim()
	if msg.statusNames != nil {
		t.statusNames = msg.statusNames
	}
	m.installJiraCards(msg.cards, msg.total, msg.err, keep)
	if msg.cached || msg.err != nil {
		return m, nil
	}
	t.fullAt, t.fullKey = time.Now(), m.jiraFetchKey(t.viewIdx)
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
	if msg.delta {
		if msg.err != nil {
			m.status = "refresh: " + msg.err.Error()
			return m, nil
		}
		prev := t.cards
		merged, added := mergeCards(prev, msg.cards)
		if len(msg.gone) > 0 {
			merged = slices.DeleteFunc(merged, func(cd jira.Card) bool { return slices.Contains(msg.gone, cd.Key) })
			added -= len(msg.gone)
		}
		m.installJiraCards(merged, max(t.total+added, len(merged)), nil, keep)
		return m, m.runRules(merged)
	}
	if !msg.cached && msg.err == nil {
		t.fullAt, t.fullKey = time.Now(), m.jiraFetchKey(msg.viewIdx)
	}
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
	q, env := jiraParseQuery(t.jiraSearchQuery()), m.jiraQueryEnv()
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
		if !jiraCardMatches(cd, q, env) {
			continue
		}
		if l, ok := col[cd.StatusID]; ok {
			t.lanes[l].cards = append(t.lanes[l].cards, i)
		}
	}
	if v.lanes {
		for _, l := range t.lanes {
			t.swim.apply(l.cards, t.cards)
			t.order = append(t.order, l.cards...)
		}
	} else {
		for i, cd := range t.cards {
			if jiraCardMatches(cd, q, env) {
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
	if t.plan != nil {
		return m.handlePlanKey(msg)
	}
	if t.charts != nil {
		return m.handleChartsKey(msg)
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
	case key.Matches(msg, m.keys.Plan):
		return m, m.openPlanning()
	case key.Matches(msg, m.keys.Charts):
		return m, m.openCharts()
	case key.Matches(msg, m.keys.Timer):
		return m, m.toggleTimer(m.selectedJiraKey())
	case key.Matches(msg, m.keys.Timesheet):
		return m, m.openTimesheet()
	case key.Matches(msg, m.keys.Inbox):
		return m, m.openInbox()
	case key.Matches(msg, m.keys.Standup):
		return m, m.openStandup()
	case key.Matches(msg, m.keys.JQL):
		return m, m.openJQL()
	case key.Matches(msg, m.keys.Site):
		m.openSitePicker()
	case key.Matches(msg, m.keys.Undo):
		return m, m.undoJiraMove()
	case key.Matches(msg, m.keys.Pin):
		if c, ok := m.selectedJiraCard(); ok {
			m.togglePin(c.Key, c.Summary)
		}
	case key.Matches(msg, m.keys.Fold):
		m.foldJiraSwimlane()
	case key.Matches(msg, m.keys.UnfoldAll):
		t.swimFold = nil
		m.renderJira()
	case key.Matches(msg, m.keys.Mark):
		m.toggleJiraMark()
	case key.Matches(msg, m.keys.MarkAll):
		m.toggleJiraMarkAll()
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
	case key.Matches(msg, m.keys.FilterBuilder):
		m.openFilterBuilder()
	case key.Matches(msg, m.keys.PanelWider):
		m.stepPanel(1)
	case key.Matches(msg, m.keys.PanelNarrower):
		m.stepPanel(-1)
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	case key.Matches(msg, m.keys.Settings):
		m.openSettings()
	case key.Matches(msg, m.keys.Sort):
		if lanes {
			keep := m.selectedJiraKey()
			switch t.swim {
			case jiraSortRank:
				t.swim = jiraSortAssignee
			case jiraSortAssignee:
				t.swim = jiraSortEpic
			case jiraSortEpic:
				t.swim = jiraSortPriority
			default:
				t.swim = jiraSortRank
			}
			t.swimFold = nil
			m.buildJiraLanes()
			m.selectJiraKey(keep)
			m.renderJira()
			m.status = "swimlanes by " + t.swim.String()
			if t.swim == jiraSortRank {
				m.status = "no swimlanes"
			}
			if m.store != nil {
				_ = m.store.SetMeta(jiraSwimKey(m.jiraBoardID()), t.swim.String())
			}
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
	case key.Matches(msg, m.keys.CopyKey) && !lanes && len(t.marked) > 0:
		return m, m.copyJiraTable()
	case key.Matches(msg, m.keys.CopyKey), key.Matches(msg, m.keys.CopyURL):
		return m, m.copyJira(m.selectedJiraKey(), key.Matches(msg, m.keys.CopyURL))
	case key.Matches(msg, m.keys.CopyBranch):
		if c, ok := m.selectedJiraCard(); ok {
			return m, m.copyBranch(c.Key, c.Type, c.Summary)
		}
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
	m.skipJiraFolded(delta)
	m.renderJira()
}

// jiraFolded is whether the lane cursor's row r of lane l is in a folded
// swimlane.
func (m *Model) jiraFolded(l, r int) bool {
	t := m.jiraTab
	if len(t.swimFold) == 0 || t.swim == jiraSortRank || l >= len(t.lanes) || r >= len(t.lanes[l].cards) {
		return false
	}
	g, _ := jiraGroupOf(t.swim, t.cards[t.lanes[l].cards[r]])
	return t.swimFold[g]
}

// skipJiraFolded moves the lane cursor off a folded swimlane: on in the
// direction of dir, else back the other way; it stays when every card is.
func (m *Model) skipJiraFolded(dir int) {
	t := m.jiraTab
	if !m.jiraShowsLanes() || !m.jiraFolded(t.lane, t.row) {
		return
	}
	step := 1
	if dir < 0 {
		step = -1
	}
	for _, s := range []int{step, -step} {
		for r := t.row + s; r >= 0 && r < len(t.lanes[t.lane].cards); r += s {
			if !m.jiraFolded(t.lane, r) {
				t.row = r
				return
			}
		}
	}
}

// foldJiraSwimlane folds the cursor's swimlane to its header.
func (m *Model) foldJiraSwimlane() {
	t := m.jiraTab
	c, ok := m.selectedJiraCard()
	if t.swim == jiraSortRank || !m.jiraShowsLanes() {
		m.status = "fold needs swimlanes (" + helpKey(m.keys.Sort) + " in lanes)"
		return
	}
	if !ok {
		return
	}
	g, _ := jiraGroupOf(t.swim, c)
	if t.swimFold == nil {
		t.swimFold = map[string]bool{}
	}
	t.swimFold[g] = true
	m.skipJiraFolded(1)
	m.renderJira()
	m.status = "folded " + g + " · " + helpKey(m.keys.UnfoldAll) + " unfolds all"
}

// moveJiraLane moves the cursor to the next lane (or previous), keeping its
// height as near as the lane allows (its swimlane, when there are).
func (m *Model) moveJiraLane(delta int) {
	t := m.jiraTab
	c, ok := m.selectedJiraCard()
	t.lane += delta
	m.clampJiraCursor()
	if ok && t.swim != jiraSortRank && t.lane < len(t.lanes) {
		// Swimlanes: stay in the card's band, on its first card there.
		g, _ := jiraGroupOf(t.swim, c)
		if r := slices.IndexFunc(t.lanes[t.lane].cards, func(ci int) bool {
			cg, _ := jiraGroupOf(t.swim, t.cards[ci])
			return cg == g
		}); r >= 0 {
			t.row = r
		}
	}
	m.skipJiraFolded(1)
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

// jiraSwimKey remembers a board's swimlanes (assignee, epic, priority or rank).
func jiraSwimKey(board int) string {
	return jiraMetaPrefix + "swim:" + strconv.Itoa(board)
}

// readJiraSwim is the current board's remembered swimlanes, none by default.
func (m *Model) readJiraSwim() jiraSort {
	if m.store == nil {
		return jiraSortRank
	}
	switch v, _, _ := m.store.GetMeta(jiraSwimKey(m.jiraBoardID())); v {
	case jiraSortAssignee.String():
		return jiraSortAssignee
	case jiraSortEpic.String():
		return jiraSortEpic
	case jiraSortPriority.String():
		return jiraSortPriority
	}
	return jiraSortRank
}

// openJiraCard shows the selected issue in the reference panel.
func (m Model) openJiraCard() (tea.Model, tea.Cmd) {
	c, ok := m.selectedJiraCard()
	if !ok {
		return m, nil
	}
	m.refBack = nil // the board is its own way back
	m.sizeRefView()
	return m.showJiraKey(c.Key)
}

// openJiraKey shows the issue key in the reference panel, the issue it
// replaces joining the trail for backspace.
func (m Model) openJiraKey(key string) (tea.Model, tea.Cmd) {
	if r := m.currentRef(); r != nil && r.jiraKey != key {
		c := refCrumb{key: r.jiraKey}
		if iss := m.jiraIssue; iss != nil && iss.Key == r.jiraKey {
			c.summary, c.status = iss.Summary, iss.Status
		}
		m.refBack = append(m.refBack, c)
		m.sizeRefView()
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
	t.lastMove = [2]string{key, cur}
	t.undoSeq++
	t.moveSeq = t.undoSeq
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

// undoJiraMove moves the last moved card back to the status it came from;
// undoing again redoes the move.
func (m *Model) undoJiraMove() tea.Cmd {
	t := m.jiraTab
	band, move := t.lastBand.key != "" && t.lastBand.seq >= t.moveSeq, t.lastMove[0] != "" && t.moveSeq >= t.lastBand.seq
	if !band && !move {
		m.status = "nothing to undo"
		return nil
	}
	var cmds []tea.Cmd
	if band {
		b := t.lastBand
		cmds = append(cmds, m.jiraSetBand(b.key, b.swim, b.prev))
	}
	if move {
		key, from := t.lastMove[0], t.lastMove[1]
		to := slices.IndexFunc(t.lanes, func(l jiraLane) bool { return slices.Contains(l.statusIDs, from) })
		if to < 0 {
			m.status = key + ": its old status is not on this board"
		} else {
			cmds = append(cmds, m.moveJiraCard(key, to, from))
		}
	}
	if band && move {
		t.lastBand.seq = t.moveSeq // redone together, as they were done
	}
	return tea.Batch(cmds...)
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
var jiraKeyStyle, jiraDimStyle, jiraOverStyle, jiraDropStyle, jiraViewActive, jiraGhostStyle, jiraPinStyle lipgloss.Style

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
// jiraAgeMark is how long an in-progress card has been so: "4d", in the
// over limit colour past stale days; "" for others and under a day.
func jiraAgeMark(c jira.Card, now time.Time, stale int) string {
	if !c.InProgress || c.Since.IsZero() {
		return ""
	}
	days := int(now.Sub(c.Since).Hours() / 24)
	switch {
	case days < 1:
		return ""
	case stale > 0 && days > stale:
		return jiraOverStyle.Render(fmt.Sprintf("%dd", days))
	}
	return jiraDimStyle.Render(fmt.Sprintf("%dd", days))
}

// jiraDueMark is an open card's due date against today: "due fri" within
// a week, "due in 12d" later, "overdue 2d" (over limit colour) past it.
func jiraDueMark(c jira.Card, now time.Time) string {
	if c.Due.IsZero() || c.Done {
		return ""
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	due := time.Date(c.Due.Year(), c.Due.Month(), c.Due.Day(), 0, 0, 0, 0, time.Local)
	days := int(math.Round(due.Sub(today).Hours() / 24))
	switch {
	case days < 0:
		return jiraOverStyle.Render(fmt.Sprintf("overdue %dd", -days))
	case days == 0:
		return jiraOverStyle.Render("due today")
	case days < 7:
		return jiraDimStyle.Render("due " + strings.ToLower(due.Format("Mon")))
	}
	return jiraDimStyle.Render(fmt.Sprintf("due in %dd", days))
}

// jiraSubtaskMark is "☑ 2/5" for a card with subtasks, "" without; all
// done shows in the done colour.
func jiraSubtaskMark(c jira.Card) string {
	if c.Subtasks == 0 {
		return ""
	}
	s := fmt.Sprintf("☑ %d/%d", c.SubtasksDone, c.Subtasks)
	if c.SubtasksDone == c.Subtasks {
		return roadmapDoneStyle.Render(s)
	}
	return jiraDimStyle.Render(s)
}

// jiraPRMark is a card's pull request sign: PR while one is open, a dim
// ✓PR once merged, nothing otherwise.
func jiraPRMark(state string) string {
	switch state {
	case "OPEN":
		return jiraViewActive.Render("PR")
	case "MERGED":
		return jiraDimStyle.Render("✓PR")
	}
	return ""
}

// jiraDeployMark is a card's top deployment environment, "▲ production".
func jiraDeployMark(env string) string {
	if env == "" {
		return ""
	}
	return jiraDimStyle.Render("▲ " + env)
}

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
		msg = jiraEmptyState("No card matches /"+t.search.Value(), "esc clears the search · "+helpKey(m.keys.FilterBuilder)+" builds a filter", w, h)
	case len(t.cards) == 0 && t.jiraFiltered():
		msg = jiraEmptyState("No card matches the filters", helpKey(m.keys.ClearFilters)+" clears them", w, h)
	case len(t.order) == 0:
		title, hint := "No issues here", helpKey(m.keys.Refresh)+" refreshes · "+helpKey(m.keys.Create)+" adds one"
		if v, ok := m.jiraCurrentView(); ok {
			switch v.kind {
			case jiraViewBacklog:
				title, hint = "The backlog is empty", helpKey(m.keys.Create)+" adds an issue"
			case jiraViewSprint:
				title, hint = "Nothing in this sprint yet", helpKey(m.keys.Plan)+" plans it from the backlog"
			}
		}
		msg = jiraEmptyState(title, hint, w, h)
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
			if i%2 == 1 { // zebra: every other row faintly shaded
				t.rows[i] = shade(t.rows[i], w)
			}
		}
		t.rowsFor = [3]int{w, keyW, stW}
	}
	var lines []string
	t.lineOf = t.lineOf[:0]
	group := ""
	for i, ci := range t.order {
		if g, ok := jiraGroupOf(t.sort, t.cards[ci]); ok && (i == 0 || g != group) {
			group = g
			lines = append(lines, m.jiraGroupHeader(g, i))
		}
		t.lineOf = append(t.lineOf, len(lines))
		row := t.rows[i]
		if i == t.idx {
			row = m.jiraListRow(t.cards[ci], true, w, keyW, stW)
		}
		lines = append(lines, row)
	}
	t.view.SetContentLinesWidth(lines, w)
	top := t.view.YOffset()
	r := 0
	if t.idx < len(t.lineOf) {
		r = t.lineOf[t.idx]
	}
	head := r // a group's first card brings its header into view
	if t.idx < len(t.lineOf) && r > 0 && (t.idx == 0 || t.lineOf[t.idx-1] != r-1) {
		head = r - 1
	}
	switch {
	case head < top:
		t.view.SetYOffset(head)
	case r >= top+h:
		t.view.SetYOffset(r - h + 1)
	}
}

// jiraGroupOf is the group a card heads under in list mode: its assignee,
// priority or epic when the list is sorted by that; ok false for others.
func jiraGroupOf(s jiraSort, c jira.Card) (string, bool) {
	switch s {
	case jiraSortAssignee:
		if c.Assignee == "" {
			return "Unassigned", true
		}
		return c.Assignee, true
	case jiraSortPriority:
		if c.Priority == "" {
			return "No priority", true
		}
		return c.Priority, true
	case jiraSortEpic:
		if c.ParentSummary == "" {
			return "No epic", true
		}
		return c.ParentSummary, true
	}
	return "", false
}

// jiraGroupHeader is the header line of the group starting at order index
// from: its name, card count and points.
func (m *Model) jiraGroupHeader(g string, from int) string {
	t := m.jiraTab
	n, pts := 0, 0.0
	for _, ci := range t.order[from:] {
		if cg, _ := jiraGroupOf(t.sort, t.cards[ci]); cg != g {
			break
		}
		n++
		if f, err := strconv.ParseFloat(t.cards[ci].Points, 64); err == nil {
			pts += f
		}
	}
	s := fmt.Sprintf("%s · %d", g, n)
	if pts > 0 {
		s += " · " + strconv.FormatFloat(pts, 'f', -1, 64) + "p"
	}
	return jiraViewActive.Render("── ") + m.jiraGroupAvatar(t.sort, g) + jiraViewActive.Render(s)
}

// jiraGroupAvatar is the chip before a group by assignee's name, "" for
// other groupings and the unassigned.
func (m *Model) jiraGroupAvatar(by jiraSort, g string) string {
	if by != jiraSortAssignee || !m.opts.fields.avatar || g == "Unassigned" {
		return ""
	}
	return jiraAvatar(g) + " "
}

func (m *Model) jiraListRow(c jira.Card, selected bool, width, keyW, stW int) string {
	status := ansi.Truncate(c.Status, stW, "…")
	status += strings.Repeat(" ", max(stW-lipgloss.Width(status), 0))
	pts := fmt.Sprintf("%3s", c.Points)
	f := m.opts.fields
	title := c.Summary
	switch {
	case f.assignee && f.avatar && c.Assignee != "":
		title += " " + jiraAvatar(c.Assignee) + jiraDimStyle.Render(" "+c.Assignee)
	case f.assignee && c.Assignee != "":
		title += jiraDimStyle.Render(" · " + c.Assignee)
	}
	if f.parent && c.ParentSummary != "" {
		title += jiraDimStyle.Render(" · ⌃ " + c.ParentSummary)
	}
	if pr := jiraPRMark(c.PR); f.pr && pr != "" {
		title = pr + " " + title
	}
	if d := jiraDeployMark(c.Deploy); f.deploy && d != "" {
		title += " " + d
	}
	if st := jiraSubtaskMark(c); f.subtasks && st != "" {
		title += " " + st
	}
	if d := jiraDueMark(c, time.Now()); f.due && d != "" {
		title += " " + d
	}
	if a := jiraAgeMark(c, time.Now(), m.opts.staleDays); f.age && a != "" {
		title += " " + a
	}
	row := "  "
	if hl := m.jiraHighlight(c.Key); hl != "" {
		row = hl + " "
	}
	row += jiraKeyStyle.Render(fmt.Sprintf("%-*s", keyW, c.Key)) + "  "
	if f.flagged && c.Flagged {
		title = jiraOverStyle.Render("⚑") + " " + title
	}
	if m.pins[c.Key] {
		title = jiraPinStyle.Render("★") + " " + title
	}
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
	if selected {
		// Plain selection colours, as the selected card: dim status, points
		// and marks sank into the selection background.
		row = ansi.Strip(row)
	}
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
	st := diffTreeSelStyle
	if m.focus == focusJira {
		st = selectedRow
	}
	row = keepBG(row, ansiOpenSeq(st))
	if pad := width - visualWidth(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	return st.Render(row)
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
	chip := ""
	if f.avatar && c.Assignee != "" {
		chip = jiraInitials(c.Assignee)
	}
	if !styled {
		return []string{key + pts, c.Summary, strings.TrimSpace(chip + " " + who)}
	}
	if chip != "" {
		chip = jiraAvatar(c.Assignee) + " "
	}
	head := jiraKeyStyle.Render(key)
	if f.flagged && c.Flagged {
		head = jiraOverStyle.Render("⚑") + " " + head
	}
	if f.typ {
		head += " " + jiraTypeIcon(c.Type)
	}
	if pm := jiraPriorityMark(c.Priority); f.priority && pm != "" {
		head += " " + pm
	}
	if pr := jiraPRMark(c.PR); f.pr && pr != "" {
		head += " " + pr
	}
	if d := jiraDeployMark(c.Deploy); f.deploy && d != "" {
		head += " " + d
	}
	if st := jiraSubtaskMark(c); f.subtasks && st != "" {
		head += " " + st
	}
	if d := jiraDueMark(c, time.Now()); f.due && d != "" {
		head += " " + d
	}
	if a := jiraAgeMark(c, time.Now(), f.stale); f.age && a != "" {
		head += " " + a
	}
	return []string{head + jiraDimStyle.Render(pts), c.Summary, chip + jiraDimStyle.Render(who)}
}

// avatarColours are the chips' backgrounds, picked per person by name.
var avatarColours = []string{"24", "29", "95", "130", "61", "66", "131", "98"}

// avatars caches jiraAvatar by name; renders run on one goroutine.
var avatars = map[string]string{}

// jiraAvatar is a person's initials on a colour of their own, the same on
// every card and every run.
func jiraAvatar(name string) string {
	if a, ok := avatars[name]; ok {
		return a
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	bg := avatarColours[h.Sum32()%uint32(len(avatarColours))]
	a := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color(bg)).Bold(true).Render(jiraInitials(name))
	avatars[name] = a
	return a
}

// jiraInitials is two letters for a name: first and last word's initials,
// or a single word's first two letters.
func jiraInitials(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return "??"
	}
	first := []rune(words[0])
	if len(words) == 1 {
		if len(first) == 1 {
			return strings.ToUpper(string(first)) + " "
		}
		return strings.ToUpper(string(first[:2]))
	}
	return strings.ToUpper(string(first[:1]) + string([]rune(words[len(words)-1])[:1]))
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
	if t.swim != jiraSortRank {
		return m.renderJiraSwimlanes(visible, laneW, height)
	}
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
		head := m.jiraLaneHead(l, inner)
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
			col = append(col, m.jiraLaneCard(c, sel, inner)...)
			col = append(col, "")
		}
		if len(slots) == 0 {
			col = append(col, jiraGhostStyle.Render(" nothing here"))
		}
		cols = append(cols, col)
	}
	sep := shade(jiraDimStyle.Render("│"), 1)
	lines := make([]string, height)
	for y := range lines {
		var b strings.Builder
		for i, col := range cols {
			cell := ""
			if y < len(col) {
				cell = col[y]
			}
			b.WriteString(canvasCell(cell, laneW-1))
			if i < len(cols)-1 {
				b.WriteString(sep)
			}
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

// jiraEmptyState is what an empty board shows: a title and what to do
// about it, centred a third of the way down.
func jiraEmptyState(title, hint string, w, h int) string {
	center := func(s string) string {
		s = ansi.Truncate(s, max(w, 1), "…")
		return strings.Repeat(" ", max((w-lipgloss.Width(s))/2, 0)) + s
	}
	pad := strings.Repeat("\n", max(h/3, 0))
	return pad + center(titleStyle.Render(title)) + "\n" + center(refDimStyle.Render(hint))
}

// canvasCell is a lane cell w wide: a card's line (full width already) on
// the terminal's background, anything else on the shaded canvas around
// the cards.
func canvasCell(cell string, w int) string {
	pad := w - lipgloss.Width(cell)
	switch {
	case pad <= 0:
		return cell
	case shadeOn:
		return shade(cell, w)
	}
	return cell + strings.Repeat(" ", pad)
}

// jiraLaneCard is a card's lines in a lane, inner wide.
func (m *Model) jiraLaneCard(c jira.Card, sel bool, inner int) []string {
	lines := jiraCardLines(c, true, m.opts.fields)
	if m.pins[c.Key] {
		lines[0] = jiraPinStyle.Render("★") + " " + lines[0]
	}
	if hl := m.jiraHighlight(c.Key); hl != "" {
		lines[0] = hl + " " + lines[0]
	}
	for i, line := range lines {
		line = ansi.Truncate(line, inner, "…")
		switch {
		case sel: // plain: dim marks vanish on the selection colour
			lines[i] = m.jiraSelect(ansi.Strip(line), true, inner)
		default: // full width, on the terminal's own background
			lines[i] = line + strings.Repeat(" ", max(inner-lipgloss.Width(line), 0))
		}
	}
	return lines
}

// renderJiraSwimlanes draws the lanes cut into swimlanes: a band per
// assignee (or epic, or priority) across every lane, headed by its name, its cards side
// by side. The bands scroll together, keeping the cursor's card in view.
func (m *Model) renderJiraSwimlanes(visible, laneW, height int) string {
	t := m.jiraTab
	inner := laneW - 1
	sep := shade(jiraDimStyle.Render("│"), 1)
	shown := t.lanes[t.firstLane:min(t.firstLane+visible, len(t.lanes))]
	row := func(cells []string) string {
		var b strings.Builder
		for i, cell := range cells {
			b.WriteString(canvasCell(cell, inner))
			if i < len(cells)-1 {
				b.WriteString(sep)
			}
		}
		return b.String()
	}
	totalW := len(shown)*laneW - 1
	heads := make([]string, len(shown))
	for i := range shown {
		heads[i] = m.jiraLaneHead(t.firstLane+i, inner)
	}
	// The groups in order: each lane is sorted by group, so a group is a run
	// in every lane; next[i] is where lane i's current run starts.
	var groups []string
	rep := map[string]jira.Card{} // a card of each group, to order them
	for _, l := range t.lanes {
		for _, ci := range l.cards {
			if g, _ := jiraGroupOf(t.swim, t.cards[ci]); rep[g].Key == "" {
				rep[g] = t.cards[ci]
				groups = append(groups, g)
			}
		}
	}
	cmp := t.swim.cmp()
	slices.SortStableFunc(groups, func(a, b string) int { return cmp(rep[a], rep[b]) })
	// Lay the body out first, then draw only the lines on screen: each line
	// is a band's header, a card row's line y (its first line at start), or
	// a gap.
	type swimLine struct {
		head  string // the band's name on its header line
		count int
		pts   string // its points, "" when none are estimated
		at    []int  // the row's card row per shown lane, -1 for none
		y     int    // the card line, -1 for a header or gap
		start int    // the row's first line
	}
	var body []swimLine
	t.swimAt, t.swimBand = t.swimAt[:0], t.swimBand[:0]
	blank := make([]int, len(shown))
	for i := range blank {
		blank[i] = -1
	}
	next := make([]int, len(shown))
	selLine := 0
	for _, g := range groups {
		runs := make([][]int, len(shown)) // per shown lane, its rows in g
		n, cards := 0, 0
		for i, l := range shown {
			for next[i] < len(l.cards) {
				if cg, _ := jiraGroupOf(t.swim, t.cards[l.cards[next[i]]]); cg != g {
					break
				}
				runs[i] = append(runs[i], next[i])
				next[i]++
			}
			n, cards = max(n, len(runs[i])), cards+len(runs[i])
		}
		if cards == 0 {
			continue
		}
		var idx []int
		for i, run := range runs {
			for _, r := range run {
				idx = append(idx, shown[i].cards[r])
			}
		}
		pts, ok := jiraLanePoints(t.cards, idx)
		if !ok || !m.opts.fields.points {
			pts = ""
		}
		body = append(body, swimLine{head: g, count: cards, pts: pts, y: -1})
		t.swimAt, t.swimBand = append(t.swimAt, blank), append(t.swimBand, rep[g])
		if t.swimFold[g] {
			continue
		}
		for r := range n {
			at := make([]int, len(shown))
			for i := range shown {
				at[i] = -1
				if r < len(runs[i]) {
					at[i] = runs[i][r]
					if t.firstLane+i == t.lane && at[i] == t.row {
						selLine = len(body)
					}
				}
			}
			start := len(body)
			for y := range jiraCardH {
				body = append(body, swimLine{at: at, y: y, start: start})
				t.swimAt, t.swimBand = append(t.swimAt, at), append(t.swimBand, rep[g])
			}
			body = append(body, swimLine{y: -1})
			t.swimAt, t.swimBand = append(t.swimAt, blank), append(t.swimBand, rep[g])
		}
	}
	h := max(height-1, 1)
	if selLine < t.swimTop+1 {
		t.swimTop = max(selLine-1, 0) // keep the band's header in view too
	}
	if selLine+jiraCardH > t.swimTop+h {
		t.swimTop = selLine + jiraCardH - h
	}
	t.swimTop = min(t.swimTop, max(len(body)-h, 0))
	cells := map[int][][]string{} // a row's drawn cards, by its first line
	lines := []string{row(heads)}
	for y := t.swimTop; y < len(body) && len(lines) < height; y++ {
		bl := body[y]
		switch {
		case bl.head != "":
			sign := "▾ "
			if t.swimFold[bl.head] {
				sign = "▸ "
			}
			n := fmt.Sprintf(" · %d", bl.count)
			if bl.pts != "" {
				n += " · " + bl.pts + "p"
			}
			lines = append(lines, shade(jiraViewActive.Render(sign)+m.jiraGroupAvatar(t.swim, bl.head)+jiraViewActive.Render(bl.head)+jiraDimStyle.Render(n), totalW))
			continue
		case bl.y < 0:
			lines = append(lines, row(make([]string, len(shown))))
			continue
		}
		cs, ok := cells[bl.start]
		if !ok {
			cs = make([][]string, len(shown))
			for i, ri := range bl.at {
				if ri < 0 {
					continue
				}
				c := t.cards[shown[i].cards[ri]]
				cs[i] = m.jiraLaneCard(c, t.firstLane+i == t.lane && ri == t.row, inner)
				if t.drag.active && c.Key == t.drag.key { // being dragged: faint where it was
					for y, line := range jiraCardLines(c, false, m.opts.fields) {
						cs[i][y] = jiraGhostStyle.Render(ansi.Truncate("┊ "+line, inner, "…"))
					}
				}
			}
			cells[bl.start] = cs
		}
		line := make([]string, len(shown))
		for i := range shown {
			if cs[i] != nil {
				line[i] = cs[i][bl.y]
			}
		}
		lines = append(lines, row(line))
	}
	for len(lines) < height {
		lines = append(lines, row(make([]string, len(shown))))
	}
	return strings.Join(lines, "\n")
}

// jiraLaneHead is lane l's heading: name, count (of its WIP limit) and
// points, lit for the cursor lane, a drop target or a limit broken.
func (m *Model) jiraLaneHead(l, inner int) string {
	t := m.jiraTab
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
	return head
}

// jiraViewSep parts the header's views.
const jiraViewSep = "  │  "

// jiraViewsFirst is the first view the header shows: 0 when they all fit,
// else late enough that the active one does, behind a ‹.
func jiraViewsFirst(views []jiraView, active, width int) int {
	if active < 0 || active >= len(views) {
		return 0
	}
	sep, w := ansi.StringWidth(jiraViewSep), 0
	for i := active; i >= 0; i-- {
		w += ansi.StringWidth(views[i].name)
		if i < active {
			w += sep
		}
		more := 0
		if i > 0 {
			more = 1 + sep // "‹" and its separator
		}
		if w+more > width {
			return min(i+1, active)
		}
	}
	return 0
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
	if tl := m.timerLabel(); tl != "" {
		meta += "  ·  " + tl
	}
	if b := m.inboxBadge(); b != "" {
		meta += "  ·  " + b + " " + helpKey(m.keys.Inbox)
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

	t.viewsFirst = jiraViewsFirst(t.views, t.viewIdx, max(boxW-3, 1)) // a cell for the truncation's …
	var views []string
	if t.viewsFirst > 0 {
		views = append(views, jiraDimStyle.Render("‹"))
	}
	for i, v := range t.views[t.viewsFirst:] {
		if i += t.viewsFirst; i == t.viewIdx {
			views = append(views, jiraViewActive.Render(v.name))
		} else {
			views = append(views, jiraDimStyle.Render(v.name))
		}
	}
	viewLine := strings.Join(views, jiraDimStyle.Render(jiraViewSep))
	if v, ok := m.jiraCurrentView(); ok {
		if bar := jiraSprintBar(t.cards); v.kind == jiraViewSprint && bar != "" {
			viewLine += "    " + bar
		}
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
	case t.charts != nil:
		viewLine = ansi.Truncate(m.chartsLine(), max(boxW-2, 1), "…")
		filterLine = ""
		body = m.renderCharts(t.view.Width(), t.view.Height())
	case t.plan != nil:
		viewLine = ansi.Truncate(m.planLine(), max(boxW-2, 1), "…")
		filterLine = ""
		body = m.renderPlan(t.view.Width(), t.view.Height())
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

// jiraSprintBar is the sprint's progress as a thin bar and a count: done
// points of all when any card has points, else done issues; "" with none.
func jiraSprintBar(cards []jira.Card) string {
	var done, total float64
	pointed := slices.ContainsFunc(cards, func(c jira.Card) bool { return c.Points != "" })
	for _, c := range cards {
		n := 1.0
		if pointed {
			n, _ = strconv.ParseFloat(c.Points, 64)
		}
		total += n
		if c.Done {
			done += n
		}
	}
	if total == 0 {
		return ""
	}
	const cells = 10
	full := int(math.Round(done / total * cells))
	unit := ""
	if pointed {
		unit = "p"
	}
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	return roadmapDoneStyle.Render(strings.Repeat("▰", full)) + jiraDimStyle.Render(strings.Repeat("▱", cells-full)+" "+num(done)+"/"+num(total)+unit)
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
		terms := jiraQueryWords(t.search.Value())
		for i, w := range terms {
			terms[i] = chip(true, w+" ×")
		}
		line = jiraDimStyle.Render("/") + strings.Join(terms, " ") + jiraDimStyle.Render(" esc") + "  " + line
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
			if i := slices.Index(t.lineOf, t.view.YOffset()+line); i >= 0 {
				return hit{zone: hitJira, idx: -1, line: i}
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
	if t.swim != jiraSortRank {
		if y := line - 1 + t.swimTop; line >= 1 && y < len(t.swimAt) && col < len(t.swimAt[y]) {
			h.line = t.swimAt[y][col]
		}
		return h
	}
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
	if t.swim != jiraSortRank {
		zone = -1 // swimlanes draw no status zones: the lane's first status
	} else if over >= 0 && over < len(t.lanes) {
		if n := len(t.lanes[over].statusIDs); n > 1 {
			zone = min(max((y-jiraBodyTop-1)/jiraZoneH(t.view.Height(), n), 0), n-1)
		}
	}
	if t.swim != jiraSortRank {
		if l := y - jiraBodyTop - 1 + t.swimTop; y-jiraBodyTop >= 1 && l < len(t.swimBand) {
			t.drag.band, t.drag.bandOK = t.swimBand[l], true
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
	band := m.jiraBandMove(d)
	if d.over == d.from && status == "" {
		m.renderJira()
		return m, band
	}
	if cmd := m.moveJiraCard(d.key, d.over, status); cmd != nil {
		if band != nil {
			t.lastBand.seq = t.moveSeq // one drop, one undo
		}
		return m, tea.Batch(cmd, band)
	}
	if band != nil {
		return m, band
	}
	m.selectJiraKey(d.key)
	m.renderJira()
	return m, nil
}

// jiraBandMove is the write a drop into another swimlane makes, as on
// Jira's board: the band's assignee (or epic) for the card. The card moves
// band at once; nil when it stays in its own, or the bands are priorities.
func (m *Model) jiraBandMove(d jiraDrag) tea.Cmd {
	if !d.bandOK {
		return nil
	}
	return m.jiraSetBand(d.key, m.jiraTab.swim, d.band)
}

// jiraBandUndo is a band drop to revert: the card's values before it.
type jiraBandUndo struct {
	key  string
	swim jiraSort
	prev jira.Card
	seq  int
}

// jiraSetBand gives card key band b's assignee (or epic, by swim), locally
// at once and in Jira; nil when it already has it.
func (m *Model) jiraSetBand(key string, swim jiraSort, b jira.Card) tea.Cmd {
	t := m.jiraTab
	ci := slices.IndexFunc(t.cards, func(c jira.Card) bool { return c.Key == key })
	if ci < 0 {
		return nil
	}
	c := &t.cards[ci]
	prev := *c
	client, ctx := m.jiraClient, m.ctx
	var field string
	var run func() error
	switch {
	case swim == jiraSortAssignee && b.AssigneeID != c.AssigneeID:
		c.Assignee, c.AssigneeID = b.Assignee, b.AssigneeID
		field, run = "assignee", func() error { return client.SetAssignee(ctx, key, b.AssigneeID) }
	case swim == jiraSortEpic && b.ParentKey != c.ParentKey:
		c.ParentKey, c.ParentSummary = b.ParentKey, b.ParentSummary
		var v any // no epic: cleared
		if b.ParentKey != "" {
			v = map[string]string{"key": b.ParentKey}
		}
		field, run = "parent", func() error { return client.SetField(ctx, key, "parent", v) }
	default:
		return nil
	}
	t.undoSeq++
	t.lastBand = jiraBandUndo{key: key, swim: swim, prev: prev, seq: t.undoSeq}
	m.buildJiraLanes()
	m.selectJiraKey(key)
	m.renderJira()
	m.status = fmt.Sprintf("updating %s %s…", key, field)
	return jiraMutateCmd(key, field, run)
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
	return m, tea.Batch(m.loadJiraDelta(), m.jiraAutoRefreshTick())
}

// jiraFetchKey names a view and the current filters, which a delta must share
// with the last whole fetch.
func (m *Model) jiraFetchKey(idx int) string {
	t := m.jiraTab
	return strconv.Itoa(idx) + "|" + jiraFilterJQL(t.assignee, t.quick, t.quickOn)
}

// loadJiraDelta fetches the current view's cards updated since the last
// fetch, when a whole fetch of the same view is recent; else all of them.
func (m *Model) loadJiraDelta() tea.Cmd {
	t := m.jiraTab
	idx := t.viewIdx
	if len(t.cards) == 0 || t.fullKey != m.jiraFetchKey(idx) || time.Since(t.fullAt) >= m.opts.fullRefresh || idx >= len(t.views) {
		return m.loadJiraCards(idx, false)
	}
	t.seq++
	t.loading = true
	// A couple of minutes' overlap covers clock skew and in-flight edits.
	mins := int(time.Since(t.fetched).Minutes()) + 2
	filter := andJQL(jiraFilterJQL(t.assignee, t.quick, t.quickOn), fmt.Sprintf("updated >= -%dm", mins))
	seq, ctx, c, board, cfg, v := t.seq, m.ctx, m.jiraClient, m.jiraBoardID(), t.cfg, t.views[idx]
	var loaded []string
	if len(t.cards) <= deltaGoneMax {
		for _, cd := range t.cards {
			loaded = append(loaded, cd.Key)
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(60*time.Second))
		defer cancel()
		cards, _, err := fetchJiraView(ctx, c, board, cfg, v, filter)
		msg := jiraCardsMsg{seq: seq, delta: true, viewIdx: idx, cards: cards, err: err}
		if err == nil && len(loaded) > 0 {
			// Loaded cards updated since but not in the view's answer have
			// left it (another sprint, a filtered-out status).
			jql := fmt.Sprintf("key in (%s) AND updated >= -%dm", strings.Join(loaded, ","), mins)
			if changed, err := c.SearchCards(ctx, jql); err == nil {
				in := map[string]bool{}
				for _, cd := range cards {
					in[cd.Key] = true
				}
				for _, cd := range changed {
					if !in[cd.Key] {
						msg.gone = append(msg.gone, cd.Key)
					}
				}
			}
		}
		return msg
	}
}

// deltaGoneMax caps the cards a partial refresh checks for having left the
// view; bigger boards wait for the whole fetch.
const deltaGoneMax = 300

// mergeCards replaces cards by key with their changed copies and adds the
// ones new to the view at the end; it returns how many were added.
func mergeCards(cards, changed []jira.Card) ([]jira.Card, int) {
	at := make(map[string]int, len(cards))
	for i, c := range cards {
		at[c.Key] = i
	}
	out := slices.Clone(cards)
	added := 0
	for _, c := range changed {
		if i, ok := at[c.Key]; ok {
			out[i] = c
			continue
		}
		out = append(out, c)
		added++
	}
	return out, added
}
