package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// Small celebrations (ui.delight, on unless off): a card moved into a done
// lane throws a short braille confetti over the lane head, a completed
// sprint says how its points compare, an empty inbox says so.

// confetti is a lane head's burst: which lane, which frame.
type confetti struct {
	lane  string // "" for none
	frame int
	seq   int // a newer burst stops the older one's ticks
}

type confettiMsg struct{ seq int }

const (
	confettiFrames = 12
	confettiEvery  = 70 * time.Millisecond
)

// confettiDots is what a burst throws: braille cells of one or two dots.
var confettiDots = []rune("⠁⠂⠄⡀⢀⠠⠐⠈⠃⠘⡠⢄⠌⠡")

// confettiStyle colours the dots. Set by applyTheme.
var confettiStyle = lipgloss.NewStyle()

func (c confetti) on(lane string) bool { return c.lane != "" && c.lane == lane }

// line is the burst's frame, w cells wide: dense at first, thinning out.
func (c confetti) line(w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	for i := range w {
		h := (i*7 + c.frame*13 + i*i*3) % 23
		if h < confettiFrames-c.frame {
			b.WriteRune(confettiDots[(i*5+c.frame*3)%len(confettiDots)])
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func confettiTick(seq int) tea.Cmd {
	return tea.Tick(confettiEvery, func(time.Time) tea.Msg { return confettiMsg{seq: seq} })
}

// celebrateMove starts a burst when key now sits in a done lane.
func (m *Model) celebrateMove(key string) tea.Cmd {
	t := m.jiraTab
	if !m.opts.delight {
		return nil
	}
	ci := slices.IndexFunc(t.cards, func(c jira.Card) bool { return c.Key == key })
	for _, lane := range t.lanes {
		// A stacked lane holds more than done: the card's own column says.
		cat := m.laneCategory(lane.col)
		if ci >= 0 && len(lane.sections) > 0 {
			cat = m.statusCategory(t.cards[ci].StatusID, cat)
		}
		if ci >= 0 && slices.Contains(lane.cards, ci) && cat == "done" {
			t.confetti = confetti{lane: lane.name, seq: t.confetti.seq + 1}
			m.renderJira()
			return confettiTick(t.confetti.seq)
		}
	}
	return nil
}

func (m Model) handleConfetti(msg confettiMsg) (tea.Model, tea.Cmd) {
	c := &m.jiraTab.confetti
	if msg.seq != c.seq || c.lane == "" {
		return m, nil
	}
	c.frame++
	var cmd tea.Cmd
	if c.frame >= confettiFrames {
		c.lane = ""
	} else {
		cmd = confettiTick(c.seq)
	}
	m.renderJira()
	return m, cmd
}

// sprintCheer is the line a completed sprint adds: its points and how they
// rank among the last closed sprints, oldest first; "" with too few.
func sprintCheer(vel []jira.SprintVelocity) string {
	if len(vel) < 2 {
		return ""
	}
	last := vel[len(vel)-1]
	if last.Done <= 0 {
		return ""
	}
	rank := 1
	for _, v := range vel[:len(vel)-1] {
		if v.Done > last.Done {
			rank++
		}
	}
	pts := strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", last.Done), "0"), ".")
	switch rank {
	case 1:
		return fmt.Sprintf("%sp, best of the last %d", pts, len(vel))
	case 2, 3:
		return fmt.Sprintf("%sp, #%d of the last %d", pts, rank, len(vel))
	}
	return pts + "p done"
}
