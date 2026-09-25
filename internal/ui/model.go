// Package ui is the Jira board TUI: matterbox's Jira tab and issue panel,
// standalone.
package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiratui/internal/config"
	"jiratui/internal/editor"
	"jiratui/internal/herdr"
	"jiratui/internal/jira"
	"jiratui/internal/store"
	"jiratui/internal/viewport"
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
	JiraComment, JiraReply, JiraStart key.Binding
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
		JiraAssignee: bind("change assignee", "a"),
		JiraComment:  bind("add comment", "c"),
		JiraReply:    bind("reply to comment", "R"),
		JiraStart:    bind("start work in a herdr worktree", "S"),
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

	jiraGotoActive bool
	jiraGotoInput  textinput.Model

	jiraPicker       jiraPickerState
	jiraPointsActive bool
	jiraPointsKey    string
	jiraPointsInput  textinput.Model

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
func New(ctx context.Context, cfg config.JiraConfig, st *store.Store) Model {
	prompt := defaultJiraStartPrompt
	if cfg.StartPrompt != "" {
		prompt = cfg.StartPrompt
	}
	m := Model{
		ctx:   ctx,
		store: st,
		keys:  defaultKeys(),
		jiraClient: jira.New(jira.Config{
			BaseURL:          cfg.BaseURL,
			Email:            cfg.Email,
			APIToken:         cfg.APIToken,
			Projects:         cfg.Projects,
			StoryPointsField: cfg.StoryPointsField,
		}),
		jiraProjects:    append([]string(nil), cfg.Projects...),
		jiraRepos:       cfg.Repos,
		jiraStartPrompt: prompt,
		jiraTab:         newJiraTabState(),
		images:          newPanelImages(),
		herdr:           herdr.Default(),
		refView:         viewport.New(),
	}
	m.refView.SoftWrap = true
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.enterJiraTab(), jiraAutoRefreshTick())
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
	case m.jiraGotoActive:
		return m.handleJiraGotoKey(msg)
	case m.jiraPicker.active:
		return m.handleJiraPickerKey(msg)
	case m.jiraPointsActive:
		return m.handleJiraPointsKey(msg)
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
	return m.helpOpen || m.jiraGotoActive || m.jiraPicker.active || m.jiraPointsActive || m.jiraCommentActive || m.jiraForm != nil
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
	case m.jiraCommentActive:
		return m.renderJiraCommentInput()
	case m.jiraPointsActive:
		return m.renderJiraPointsInput()
	case m.jiraPicker.active:
		return m.renderJiraPicker(bodyH)
	case m.jiraForm != nil:
		return m.renderJiraForm()
	}
	return ""
}
