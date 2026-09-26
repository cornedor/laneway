// Package ui is the Jira board TUI: matterbox's Jira tab and issue panel,
// standalone.
package ui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/viewport"
)

type focus int

const (
	focusJira focus = iota
	focusRef
)

type keyMap struct {
	Tab, ShiftTab                     key.Binding
	Up, Down, Left, Right             key.Binding
	Home, End                         key.Binding
	InputUp, InputDown                key.Binding
	PageUp, PageDown                  key.Binding
	OpenChannel, OpenRef              key.Binding
	OpenAttach, Refresh               key.Binding
	JiraStatus, JiraPriority          key.Binding
	JiraPoints, JiraAssignee          key.Binding
	JiraSummary, JiraLabels           key.Binding
	JiraComment, JiraReply, JiraStart key.Binding
	JiraLinks, Back, Image            key.Binding

	// The board's own keys.
	Quit, Help, Search, Goto, Create   key.Binding
	CopyKey, CopyURL                   key.Binding
	MoveCardLeft, MoveCardRight        key.Binding
	Project, Board, NextView, PrevView key.Binding
	ToggleMode, Sort, MoveSprint       key.Binding
	Assignee, Mine, ClearFilters       key.Binding
	Roadmap, Palette, Mark, Bulk, Plan key.Binding
	MarkAll, Undo                      key.Binding
	Charts, LogWork, Timer, Timesheet  key.Binding
	JiraDescription, Inbox             key.Binding
	IssueActions, Site, Standup        key.Binding
	History, DevInfo, JQL, Pin         key.Binding
	Fold, UnfoldAll                    key.Binding
}

func bind(help string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], help))
}

func defaultKeys() keyMap {
	return keyMap{
		Tab:          bind("focus next", "tab"),
		ShiftTab:     bind("focus prev", "shift+tab"),
		Up:           bind("up", "up", "k"),
		Down:         bind("down", "down", "j"),
		Left:         bind("left", "left", "h"),
		Right:        bind("right", "right", "l"),
		Home:         bind("top", "home", "g"),
		End:          bind("bottom", "end", "G"),
		InputUp:      bind("up", "up", "ctrl+p"),
		InputDown:    bind("down", "down", "ctrl+n"),
		PageUp:       bind("page up", "pgup", "ctrl+u"),
		PageDown:     bind("page down", "pgdown", "ctrl+d"),
		OpenChannel:  bind("open", "enter"),
		OpenRef:      bind("open issue", "v"),
		OpenAttach:   bind("open in browser", "o"),
		Refresh:      bind("refresh", "r"),
		JiraStatus:   bind("change status", "s"),
		JiraPriority: bind("change priority", "p"),
		JiraPoints:   bind("set story points", "P"),
		JiraSummary:  bind("edit summary", "e"),
		JiraLabels:   bind("edit labels", "l"),
		JiraAssignee: bind("change assignee", "a"),
		JiraComment:  bind("add comment", "c"),
		JiraReply:    bind("reply to comment", "R"),
		JiraStart:    bind("start work in a herdr worktree", "S"),
		JiraLinks:    bind("go to linked issue", "L"),
		Back:         bind("previous issue", "backspace"),
		Image:        bind("view images", "i"),

		Quit:            bind("quit", "q"),
		Help:            bind("help", "?"),
		Search:          bind("search", "/"),
		Goto:            bind("go to issue by key", "#"),
		Create:          bind("new issue", "n"),
		CopyKey:         bind("copy key", "y"),
		CopyURL:         bind("copy URL", "Y"),
		MoveCardLeft:    bind("move card left", "H", "shift+left"),
		MoveCardRight:   bind("move card right", "L", "shift+right"),
		Project:         bind("project", "p"),
		Board:           bind("board", "b"),
		NextView:        bind("next view", "]"),
		PrevView:        bind("previous view", "["),
		ToggleMode:      bind("lanes / list", "t"),
		MoveSprint:      bind("move to sprint / backlog", "M"),
		Sort:            bind("sort the list", "s"),
		Assignee:        bind("assignee filter", "a"),
		Mine:            bind("only mine", "m"),
		ClearFilters:    bind("clear filters", "0"),
		Roadmap:         bind("roadmap", "R"),
		Palette:         bind("command palette", ":"),
		Mark:            bind("mark card", "x"),
		MarkAll:         bind("mark the lane / every row", "X"),
		Undo:            bind("undo the last card move", "u"),
		Bulk:            bind("edit marked cards", "B"),
		Plan:            bind("sprint planning", "P"),
		Charts:          bind("sprint charts", "C"),
		LogWork:         bind("log work", "w"),
		JiraDescription: bind("edit description in $EDITOR", "E"),
		Timer:           bind("start / stop the timer", "T"),
		Timesheet:       bind("today's worklogs", "W"),
		Inbox:           bind("inbox", "I"),
		IssueActions:    bind("subtask, link, clone, watch", "A"),
		Site:            bind("switch Jira site", "@"),
		Standup:         bind("standup: what you did", "U"),
		History:         bind("issue history", "H"),
		DevInfo:         bind("pull requests and branches", "D"),
		Pin:             bind("pin / unpin issue", "*"),
		Fold:            bind("fold the swimlane", "z"),
		UnfoldAll:       bind("unfold every swimlane", "Z"),
		JQL:             bind("JQL search", "Q"),
	}
}

// hit is what a screen cell holds: on the board, idx is the lane (-1 in list
// mode) and line the card's row in it, -1 over no card.
type hitZone int

const (
	hitNone hitZone = iota
	hitJira
	hitRef
)

type hit struct {
	zone      hitZone
	idx, line int
}

// Model is the whole app. Held by value like matterbox's; the board state
// sits behind a pointer.
type Model struct {
	ctx   context.Context
	store *store.Store
	// pins are the pinned issue keys (palette.go).
	pins   map[string]bool
	keys   keyMap
	width  int
	height int
	focus  focus
	status string

	// rules fire on board changes, logged to rulesLog.
	rules    *rules.Set
	rulesLog string

	jiraClient      *jira.Client
	jiraProjects    []string
	jiraRepos       map[string]string
	jiraStartPrompt string
	jiraStarting    map[string]bool
	herdr           *herdr.Client
	emojiImg        *emojiImages

	jiraTab  *jiraTabState
	jiraForm *jiraFormState

	// The issue panel.
	refOpen    bool
	refs       []reference
	refIdx     int
	refBack    []refCrumb // issues the panel came from by links, newest last (ref.go)
	refGen     int
	refLoading bool
	refErr     error
	refView    viewport.Model
	jiraIssue  *jira.Issue
	panelHint  string

	helpOpen bool
	images   *panelImages
	opts     options

	jiraGotoActive bool
	jiraGotoInput  textinput.Model

	// imageView shows the panel's images full size (image_view.go).
	imageView    bool
	imageViewIdx int

	jiraCreateActive bool
	jiraCreateType   string
	// jiraCreateParent is the issue a new subtask or epic child goes under,
	// in jiraCreateProject; "" for a plain new issue.
	jiraCreateParent, jiraCreateProject string
	// jiraCreateReload reloads the roadmap once the new issue (an epic
	// made from it) exists.
	jiraCreateReload bool
	// jiraLinkChoice is the link type and direction picked for "link".
	jiraLinkChoice  jiraPickerItem
	jiraCreateInput textinput.Model

	jiraPicker jiraPickerState
	// fieldCursor is the panel's selected field (panel_fields.go), -1 for
	// none; it holds only while fieldCursorKey is the shown issue.
	fieldCursor    int
	fieldCursorKey string
	// paletteFocus is the pane the palette was opened from; its actions
	// run there.
	paletteFocus focus
	// timer runs on an issue (worklog.go); worklogStart is when the work
	// being logged began, zero for "back from now".
	timer        workTimer
	worklogStart time.Time
	// worklogEdit is the worklog the input edits (id, on worklogEditDay's
	// timesheet), "" for a new one.
	worklogEdit    string
	worklogEditDay time.Time
	// worklogFromTimer marks the input as the timer's stop: the timer ends
	// once the log is written.
	worklogFromTimer bool
	// sites are the configured Jira sites ("" is jira:), site the shown
	// one; nextSite is set when the app ends to switch (sites.go).
	sites    []string
	site     string
	nextSite *string
	// prefetchSeq debounces the cursor's prefetch (prefetch.go).
	prefetchSeq int
	// jql is the open JQL search input (jql.go).
	jql *jqlState
	// jiraCommentMentions are the people @-completed in the composer, and
	// jiraMention its open completion (mention.go).
	jiraCommentMentions []jira.Mention
	jiraMention         mentionState
	// inboxUnread is the header's count of issues with news (inbox.go);
	// mentionsSeen the newest mention notified, started when the app began.
	inboxUnread  int
	mentionsSeen time.Time
	started      time.Time
	// panelExtra is panelExtraKey's other editable fields (editmeta);
	// panelEditID is the one being edited.
	panelExtra    []jiraFormField
	panelExtraKey string
	// panelHits are the panel's clickable lines by content line: a field's
	// index or a linked issue's key (panel_mouse.go); panelFieldLine is each
	// field's line as the last render wrote it.
	panelHits      map[int]panelHit
	panelFieldLine []int
	panelEditID    string

	// The one-line field input: story points, the summary or labels.
	jiraFieldActive bool
	jiraFieldName   string // "points", "summary", "labels" or "field" (panelEditID)
	jiraFieldKey    string
	jiraFieldInput  textinput.Model

	jiraCommentActive  bool
	jiraCommentKey     string
	jiraCommentInput   editor.Model
	jiraCommentMention *jira.Mention
	jiraCommentReplyTo string

	// lastClick detects a double-click.
	lastClick struct {
		at   time.Time
		x, y int
	}
}

// New builds the app from the jira: config.
func New(ctx context.Context, cfg config.JiraConfig, ui config.UIConfig, rs []rules.Rule, rulesLog string, st *store.Store) Model {
	opts, warn := optionsFrom(ui)
	keys := defaultKeys()
	warn = append(warn, keys.applyKeys(ui.Keys)...)
	th, thWarn := themeFrom(ui.Theme)
	applyTheme(th)
	warn = append(warn, thWarn...)
	ruleSet, ruleWarn := rules.Compile(rs)
	warn = append(warn, ruleWarn...)
	prompt := defaultJiraStartPrompt
	if cfg.StartPrompt != "" {
		prompt = cfg.StartPrompt
	}
	m := Model{
		ctx:   ctx,
		store: st,
		keys:  keys,
		jiraClient: jira.New(jira.Config{
			BaseURL:          cfg.BaseURL,
			Email:            cfg.Email,
			APIToken:         cfg.APIToken,
			Projects:         cfg.Projects,
			StoryPointsField: cfg.StoryPointsField,
			CardLimit:        opts.cardLimit,
		}),
		jiraProjects:    append([]string(nil), cfg.Projects...),
		jiraRepos:       cfg.Repos,
		jiraStartPrompt: prompt,
		jiraTab:         newJiraTabState(),
		images:          newPanelImages(opts.images, opts.imageMaxRows),
		opts:            opts,
		rules:           ruleSet,
		rulesLog:        rulesLog,
		status:          strings.Join(warn, " · "),
		herdr:           herdr.Default(),
		refView:         viewport.New(),
		fieldCursor:     -1,
		started:         time.Now(),
	}
	m.refView.SoftWrap = true
	m.jiraTab.wantLanes = opts.lanes
	m.loadPins()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.enterJiraTab(), m.jiraAutoRefreshTick(), m.queryCellSize(), m.startRuleWatches(), m.loadTimer(), m.countInbox(), inboxTick())
}

// bodyH is the rows above the status line.
func (m *Model) bodyH() int {
	return max(m.height-1, 5)
}

// resize lays the board and the panel out for the current size.
func (m *Model) resize() {
	bodyH := m.bodyH()
	m.sizeJiraView(m.width, bodyH-1)
	_, refW := m.jiraListWidth(m.width)
	m.refView.SetWidth(max(refW-4, 1))
	m.sizeRefView()
	m.renderJira()
	m.renderRef()
	m.fitImageView()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	out, cmd := m.update(msg)
	if om, ok := out.(Model); ok {
		if f := om.flushImages(); f != nil {
			return om, tea.Batch(cmd, f)
		}
	}
	return out, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case tea.MouseMotionMsg:
		if m.jiraDragging() && msg.Button == tea.MouseLeft {
			return m.dragJira(msg.X, msg.Y)
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft && m.jiraDragging() {
			return m.dropJira()
		}
		return m, nil
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)
	case tea.PasteMsg:
		if m.jiraCommentActive {
			var cmd tea.Cmd
			m.jiraCommentInput, cmd = m.jiraCommentInput.Update(msg)
			return m, cmd
		}
		return m, nil

	case jiraBoardMsg:
		return m.handleJiraBoard(msg)
	case jiraCardsMsg:
		return m.handleJiraCards(msg)
	case jiraMovedMsg:
		return m.handleJiraMoved(msg)
	case jiraLoadedMsg:
		return m.handleJiraLoaded(msg)
	case jiraDownloadedMsg:
		return m.handleJiraDownloaded(msg)
	case jiraVoteMsg:
		return m.handleJiraVote(msg)
	case jiraWatchMsg:
		return m.handleJiraWatch(msg)
	case descLoadedMsg:
		return m.handleDescLoaded(msg)
	case descEditedMsg:
		return m.handleDescEdited(msg)
	case mentionSearchMsg:
		return m.handleMentionSearch(msg)
	case mentionFoundMsg:
		return m.handleMentionFound(msg)
	case jqlWordsMsg:
		return m.handleJQLWords(msg)
	case jqlValuesMsg:
		return m.handleJQLValues(msg)
	case inboxTickMsg:
		return m.handleInboxTick()
	case inboxCountMsg:
		return m.handleInboxCount(msg)
	case inboxMentionsMsg:
		return m.handleInboxMentions(msg)
	case paletteSearchMsg:
		return m.handlePaletteSearch(msg)
	case paletteFoundMsg:
		return m.handlePaletteFound(msg)
	case prefetchMsg:
		return m.handlePrefetch(msg)
	case worklogLoggedMsg:
		return m.handleWorklogLogged(msg)
	case timerTickMsg:
		return m.handleTimerTick()
	case chartsMsg:
		return m.handleCharts(msg)
	case planMsg:
		return m.handlePlan(msg)
	case planSprintMsg:
		return m.handlePlanSprint(msg)
	case planWroteMsg:
		return m.handlePlanWrote(msg)
	case bulkMoveMsg:
		return m.handleBulkMove(msg)
	case bulkDoneMsg:
		return m.handleBulkDone(msg)
	case roadmapMsg:
		return m.handleRoadmap(msg)
	case roadmapSaveMsg:
		return m.handleRoadmapSave(msg)
	case roadmapSavedMsg:
		return m.handleRoadmapSaved(msg)
	case panelExtraMsg:
		return m.handlePanelExtra(msg)
	case jiraPickerLoadedMsg:
		return m.handleJiraPickerLoaded(msg)
	case jiraAssigneeDebounceMsg:
		return m.handleJiraAssigneeDebounce(msg)
	case jiraMutatedMsg:
		return m.handleJiraMutated(msg)
	case jiraPreparedMsg:
		return m.handleJiraPrepared(msg)
	case jiraFormDoneMsg:
		return m.handleJiraFormDone(msg)
	case jiraWorkMsg:
		return m.handleJiraWork(msg)
	case imageLoadedMsg:
		return m.handleImageLoaded(msg)
	case jiraAutoRefreshMsg:
		return m.handleJiraAutoRefresh()
	case uv.CellSizeEvent:
		return m.handleCellSize(msg)
	case jiraCreatedMsg:
		return m.handleJiraCreated(msg)
	case ruleWatchMsg:
		return m, m.pollRuleWatch(msg.jql)
	case ruleWatchedMsg:
		return m.handleRuleWatched(msg)
	case rulesEventsMsg:
		return m.handleRulesEvents(msg)
	case rulesLoggedMsg:
		if msg.err != nil {
			m.status = "rule: " + msg.err.Error()
		}
		return m, nil
	case openedMsg:
		if msg.err != nil {
			m.status = "open " + msg.name + ": " + msg.err.Error()
		}
		return m, nil
	}
	return m, nil
}

// handleKey routes a key to the modal that owns the keyboard, else the
// focused pane.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.helpOpen:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		m.helpOpen = false
		return m, nil
	case m.imageView:
		return m.handleImageViewKey(msg)
	case m.jql != nil:
		return m.handleJQLKey(msg)
	case m.jiraGotoActive:
		return m.handleJiraGotoKey(msg)
	case m.jiraCreateActive:
		return m.handleJiraCreateKey(msg)
	case m.jiraPicker.active:
		return m.handleJiraPickerKey(msg)
	case m.jiraFieldActive:
		return m.handleJiraFieldKey(msg)
	case m.jiraCommentActive:
		return m.handleJiraCommentKey(msg)
	case m.jiraForm != nil:
		return m.handleJiraFormKey(msg)
	case m.focus == focusRef && m.refOpen:
		return m.handleRefKey(msg)
	case m.jiraTab.searching:
		return m.handleJiraSearchKey(msg)
	}
	before := m.selectedJiraKey()
	out, cmd := m.handleJiraKey(msg)
	if om, ok := out.(Model); ok && om.jiraTab.roadmap == nil && om.jiraTab.plan == nil && om.jiraTab.charts == nil {
		if after := om.selectedJiraKey(); after != "" && after != before {
			return om, tea.Batch(cmd, om.schedulePrefetch())
		}
	}
	return out, cmd
}

func (m *Model) modalOpen() bool {
	return m.helpOpen || m.imageView || m.jql != nil || m.jiraGotoActive || m.jiraCreateActive || m.jiraPicker.active || m.jiraFieldActive || m.jiraCommentActive || m.jiraForm != nil
}

func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseLeft && m.pickerOnTop() {
		// A click picks the row (as enter would); outside the box, cancels.
		switch i, outside := m.pickerRowAt(msg.X, msg.Y); {
		case outside:
			return m.handleJiraPickerKey(keyPress("esc"))
		case i >= 0:
			m.jiraPicker.idx = i
			return m.handleJiraPickerKey(keyPress("enter"))
		}
		return m, nil
	}
	if msg.Button != tea.MouseLeft || m.modalOpen() || msg.Y >= m.bodyH() {
		return m, nil
	}
	count := 1
	if time.Since(m.lastClick.at) < 400*time.Millisecond && m.lastClick.x == msg.X && m.lastClick.y == msg.Y {
		count = 2
	}
	m.lastClick.at, m.lastClick.x, m.lastClick.y = time.Now(), msg.X, msg.Y
	if count == 2 {
		m.lastClick.at = time.Time{}
	}
	if listW, _ := m.jiraListWidth(m.width); m.refOpen && msg.X >= listW {
		m.focus = focusRef
		if i := m.crumbAt(msg.Y); i >= 0 {
			return m.backToCrumb(i)
		}
		if u := m.panelLinkAt(msg.X, msg.Y); u != "" {
			return m.clickPanel(panelHit{field: -1, url: u}, count)
		}
		if h, ok := m.panelHits[m.panelLineAt(msg.Y)]; ok {
			return m.clickPanel(h, count)
		}
		m.renderJira()
		return m, nil
	}
	if out, cmd, ok := m.clickHeader(msg.X, msg.Y); ok {
		return out, cmd
	}
	switch t := m.jiraTab; {
	case t.roadmap != nil:
		m.focus = focusJira
		return m.clickRoadmap(msg.Y, count)
	case t.plan != nil:
		m.focus = focusJira
		return m.clickPlan(msg.X, msg.Y, count)
	case t.charts != nil:
		return m, nil
	}
	return m.clickJira(m.hitJira(msg.X, msg.Y), msg.X, msg.Y, count)
}

func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.pickerOnTop() {
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.handleJiraPickerKey(keyPress("up"))
		case tea.MouseWheelDown:
			return m.handleJiraPickerKey(keyPress("down"))
		}
		return m, nil
	}
	if m.modalOpen() {
		return m, nil
	}
	delta := 3
	if msg.Button == tea.MouseWheelUp {
		delta = -3
	} else if msg.Button != tea.MouseWheelDown {
		return m, nil
	}
	if listW, _ := m.jiraListWidth(m.width); m.refOpen && msg.X >= listW {
		m.refView.SetYOffset(m.refView.YOffset() + delta)
		return m, nil
	}
	if t := m.jiraTab; t.roadmap != nil || t.plan != nil {
		m.wheelView(msg.X, delta/3)
		return m, nil
	}
	if m.jiraShowsLanes() {
		// Over another lane the wheel scrolls it; the cursor's lane (and
		// swimlanes, which scroll together) moves the cursor.
		t := m.jiraTab
		if h := m.hitJira(msg.X, msg.Y); h.idx >= 0 && h.idx != t.lane && t.swim == jiraSortRank && h.idx < len(t.laneTop) {
			t.laneTop[h.idx] = min(max(t.laneTop[h.idx]+delta/3, 0), max(len(t.lanes[h.idx].cards)-1, 0))
			m.renderJira()
			return m, nil
		}
		m.moveJiraCursor(delta / 3)
		return m, nil
	}
	m.jiraTab.view.SetYOffset(m.jiraTab.view.YOffset() + delta)
	return m, nil
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	if m.width == 0 {
		return v
	}
	bodyH := m.bodyH()
	body := m.renderJiraPane(bodyH, m.width)
	if m.imageView {
		body = m.renderImageView(m.width, bodyH)
	}
	if ov := m.renderOverlay(bodyH); ov != "" {
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, ov)
	}
	status := statusStyle.Render(ansi.Truncate(" "+m.status, m.width, "…"))
	v.SetContent(lipgloss.JoinVertical(lipgloss.Left, body, status))
	if m.jiraCommentActive {
		above := 0
		if m.jiraCommentReplyTo != "" {
			above = 1
		}
		if cx, cy, ok := m.modalComposerCursor(above, &m.jiraCommentInput); ok {
			v.Cursor = tea.NewCursor(cx, cy)
		}
	}
	return v
}

// pickerOnTop is whether the picker is the modal drawn (renderOverlay).
func (m *Model) pickerOnTop() bool {
	return m.jiraPicker.active && !m.helpOpen && m.jql == nil && !m.jiraGotoActive && !m.jiraCreateActive &&
		!m.jiraCommentActive && !m.jiraFieldActive
}

// renderOverlay draws the open modal, last one winning as in matterbox.
func (m *Model) renderOverlay(bodyH int) string {
	switch {
	case m.helpOpen:
		return m.renderHelp(bodyH)
	case m.jql != nil:
		return m.renderJQL()
	case m.jiraGotoActive:
		return m.renderJiraGoto()
	case m.jiraCreateActive:
		return m.renderJiraCreate()
	case m.jiraCommentActive:
		return m.renderJiraCommentInput()
	case m.jiraFieldActive:
		return m.renderJiraFieldInput()
	case m.jiraPicker.active:
		return m.renderJiraPicker(bodyH)
	case m.jiraForm != nil:
		return m.renderJiraForm()
	}
	return ""
}
