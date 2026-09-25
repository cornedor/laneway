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
	JiraSummary                       key.Binding
	JiraComment, JiraReply, JiraStart key.Binding
	JiraLinks, Back, Image            key.Binding

	// The board's own keys.
	Quit, Help, Search, Goto, Create   key.Binding
	CopyKey, CopyURL                   key.Binding
	MoveCardLeft, MoveCardRight        key.Binding
	Project, Board, NextView, PrevView key.Binding
	ToggleMode, Sort                   key.Binding
	Assignee, Mine, ClearFilters       key.Binding
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
		JiraAssignee: bind("change assignee", "a"),
		JiraComment:  bind("add comment", "c"),
		JiraReply:    bind("reply to comment", "R"),
		JiraStart:    bind("start work in a herdr worktree", "S"),
		JiraLinks:    bind("go to linked issue", "L"),
		Back:         bind("previous issue", "backspace"),
		Image:        bind("view images", "i"),

		Quit:          bind("quit", "q"),
		Help:          bind("help", "?"),
		Search:        bind("search", "/"),
		Goto:          bind("go to issue by key", "#"),
		Create:        bind("new issue", "n"),
		CopyKey:       bind("copy key", "y"),
		CopyURL:       bind("copy URL", "Y"),
		MoveCardLeft:  bind("move card left", "H", "shift+left"),
		MoveCardRight: bind("move card right", "L", "shift+right"),
		Project:       bind("project", "p"),
		Board:         bind("board", "b"),
		NextView:      bind("next view", "]"),
		PrevView:      bind("previous view", "["),
		ToggleMode:    bind("lanes / list", "t"),
		Sort:          bind("sort the list", "s"),
		Assignee:      bind("assignee filter", "a"),
		Mine:          bind("only mine", "m"),
		ClearFilters:  bind("clear filters", "0"),
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
	ctx    context.Context
	store  *store.Store
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
	refBack    []string // issues the panel showed before, newest last
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
	jiraCreateInput  textinput.Model

	jiraPicker jiraPickerState
	// The one-line field input: story points or the summary.
	jiraFieldActive bool
	jiraFieldName   string // "points" or "summary"
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
	}
	m.refView.SoftWrap = true
	m.jiraTab.wantLanes = opts.lanes
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.enterJiraTab(), m.jiraAutoRefreshTick(), m.queryCellSize(), m.startRuleWatches())
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
	m.refView.SetHeight(max(bodyH-2, 1))
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
	return m.handleJiraKey(msg)
}

func (m *Model) modalOpen() bool {
	return m.helpOpen || m.imageView || m.jiraGotoActive || m.jiraCreateActive || m.jiraPicker.active || m.jiraFieldActive || m.jiraCommentActive || m.jiraForm != nil
}

func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
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
		m.renderJira()
		return m, nil
	}
	return m.clickJira(m.hitJira(msg.X, msg.Y), msg.X, msg.Y, count)
}

func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
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
	if m.jiraShowsLanes() {
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

// renderOverlay draws the open modal, last one winning as in matterbox.
func (m *Model) renderOverlay(bodyH int) string {
	switch {
	case m.helpOpen:
		return m.renderHelp()
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
