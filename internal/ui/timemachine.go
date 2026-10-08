package ui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// The board's time machine: ctrl+t, then ← and → step the lanes back and
// forth a day at a time, each card where its status changelog had it at
// the end of that day; cards made later drop out. esc comes back to now.
// It only looks: keys that change a card wait until you are back.

// timeMachine is the replay's state.
type timeMachine struct {
	moves   map[string][]jira.StatusMove // the cards' status changes, by key
	loading bool
	days    int // how many days back; the board shows the end of that day
	seq     int
	// at pins the replay to one moment instead (a closed sprint's close);
	// zero steps by days.
	at time.Time
}

// timeMachineMax is how far back ← goes, in days.
const timeMachineMax = 90

type timeMachineMsg struct {
	seq   int
	moves map[string][]jira.StatusMove
	err   error
}

// openTimeMachine loads the cards' changelogs and steps back a day.
func (m *Model) openTimeMachine() tea.Cmd { return m.openTimeMachineAt(time.Time{}) }

// openTimeMachineAt is openTimeMachine pinned at at; zero steps by days.
func (m *Model) openTimeMachineAt(at time.Time) tea.Cmd {
	t := m.jiraTab
	if !m.jiraShowsLanes() {
		if v, ok := m.jiraCurrentView(); !ok || !v.lanes || !v.closed.IsZero() {
			m.status = i18n.T("the time machine needs a sprint or board view")
			return nil
		}
		keep := m.selectedJiraKey()
		t.wantLanes, t.pastFromList = true, true // leaving goes back to the list
		m.selectJiraKey(keep)
	}
	keys := make([]string, len(t.cards))
	for i, c := range t.cards {
		keys[i] = c.Key
	}
	seq := 1
	if t.past != nil {
		seq = t.past.seq + 1
	}
	t.past = &timeMachine{loading: true, days: 1, seq: seq, at: at}
	m.status = i18n.T("reading the board's history…")
	m.renderJira()
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		moves, err := c.StatusMoves(ctx, keys)
		return timeMachineMsg{seq: seq, moves: moves, err: err}
	}
}

func (m Model) handleTimeMachine(msg timeMachineMsg) (tea.Model, tea.Cmd) {
	p := m.jiraTab.past
	if p == nil || p.seq != msg.seq {
		return m, nil
	}
	if msg.err != nil {
		m.jiraTab.past = nil
		m.fail(i18n.Tf("time machine: %s", msg.err.Error()))
		m.renderJira()
		return m, nil
	}
	p.moves, p.loading = msg.moves, false
	m.stepTimeMachine(0)
	return m, nil
}

// asOf is the moment the board replays: the end of the day days back.
func (p *timeMachine) asOf(now time.Time) time.Time {
	if !p.at.IsZero() {
		return p.at
	}
	y, mo, d := now.Date()
	return time.Date(y, mo, d-p.days+1, 0, 0, 0, 0, now.Location())
}

// pastLabel is the header's "as of mon 22".
func (p *timeMachine) label(now time.Time) string {
	if p.loading {
		return i18n.T("⏲ reading history…")
	}
	if !p.at.IsZero() {
		return i18n.Tf("⏲ as it closed, %s", strings.ToLower(p.at.Local().Format("Mon 2 Jan")))
	}
	return i18n.Tf("⏲ as of %s", strings.ToLower(p.asOf(now).Add(-time.Minute).Format("Mon 2 Jan")))
}

// pastCard is c as the time machine has it, false when it didn't exist yet.
func (p *timeMachine) card(c jira.Card, asOf time.Time) (jira.Card, bool) {
	if !c.Created.IsZero() && !c.Created.Before(asOf) {
		return c, false
	}
	c.StatusID = jira.BurnIssue{Status: c.StatusID, Moves: p.moves[c.Key]}.StatusAt(asOf)
	return c, true
}

// stepTimeMachine moves d days further back (negative: forward); back at
// today it closes.
func (m *Model) stepTimeMachine(d int) {
	t := m.jiraTab
	p := t.past
	p.days = min(p.days+d, timeMachineMax)
	if p.days <= 0 {
		m.closeTimeMachine()
		return
	}
	keep := m.selectedJiraKey()
	m.buildJiraLanes()
	m.selectJiraKey(keep)
	m.status = i18n.T("← earlier · → later · esc back to now")
	if !p.at.IsZero() {
		m.status = i18n.T("a closed sprint, as it closed · esc leaves it")
	}
	m.renderJira()
}

func (m *Model) closeTimeMachine() {
	keep := m.selectedJiraKey()
	m.jiraTab.past = nil
	if m.jiraTab.pastFromList {
		m.jiraTab.wantLanes, m.jiraTab.pastFromList = false, false
	}
	m.buildJiraLanes()
	m.selectJiraKey(keep)
	m.status = i18n.T("back to now")
	m.renderJira()
}

// handleTimeMachineKey owns the board's keys while it replays: ← → step,
// the cursor keys and enter still work, esc leaves.
func (m Model) handleTimeMachineKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.jiraTab.past
	switch s := msg.String(); {
	case s == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m.quit()
	case !p.at.IsZero() && (s == "esc" || key.Matches(msg, m.keys.TimeMachine) || key.Matches(msg, m.keys.ClosedSprint)):
		return m, m.leaveClosedSprint()
	case s == "esc", key.Matches(msg, m.keys.TimeMachine):
		m.closeTimeMachine()
	case p.loading:
		m.status = i18n.T("still reading the history · esc cancels")
	case !p.at.IsZero() && (s == "left" || s == "right"):
		m.status = i18n.T("a closed sprint shows as it closed · esc leaves it")
	case s == "left":
		m.stepTimeMachine(1)
	case s == "right":
		m.stepTimeMachine(-1)
	case key.Matches(msg, m.keys.Up):
		m.moveJiraCursor(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveJiraCursor(1)
	case key.Matches(msg, m.keys.Left):
		m.moveJiraLane(-1)
	case key.Matches(msg, m.keys.Right):
		m.moveJiraLane(1)
	case key.Matches(msg, m.keys.OpenChannel), key.Matches(msg, m.keys.OpenRef):
		return m.openJiraCard()
	case !p.at.IsZero():
		m.status = i18n.T("a closed sprint only looks · esc leaves it")
	default:
		m.status = i18n.T("the time machine only looks · esc back to now")
	}
	return m, nil
}
