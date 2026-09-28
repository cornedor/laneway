package ui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Bubble Tea draws a frame after every message, and a trackpad sends wheel
// notches faster than a frame draws: they queued, and the scroll ran on
// after the fingers lifted. WheelFilter lets a burst's first notch through
// and folds the rest into one message a frame.

// wheelFrame is how often a burst's notches are let through.
const wheelFrame = time.Second / 60

// wheelBatchMsg is n notches of wheel, over one spot.
type wheelBatchMsg struct {
	wheel tea.MouseWheelMsg
	n     int
}

// wheelFlushMsg ends a frame of a burst.
type wheelFlushMsg struct{}

type wheelCoalescer struct {
	mu      sync.Mutex
	send    func(tea.Msg)
	open    bool // a frame is running: notches wait for its end
	pending tea.MouseWheelMsg
	net     int // notches down, less those up
}

// WheelFilter is a tea.WithFilter that coalesces wheel bursts; send is the
// program's Send, which ends each frame.
func WheelFilter(send func(tea.Msg)) func(tea.Model, tea.Msg) tea.Msg {
	c := &wheelCoalescer{send: send}
	return func(_ tea.Model, msg tea.Msg) tea.Msg { return c.filter(msg) }
}

func (c *wheelCoalescer) filter(msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		d := wheelDir(msg)
		if d == 0 {
			return msg
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.open {
			c.open = true
			c.tick()
			return msg
		}
		if c.net != 0 && !sameSpot(c.pending, msg) {
			out := c.take()
			c.pending, c.net = msg, d
			return out
		}
		c.pending, c.net = msg, c.net+d
		return nil
	case wheelFlushMsg:
		c.mu.Lock()
		defer c.mu.Unlock()
		out := c.take()
		if out == nil {
			c.open = false // a quiet frame: the next notch goes straight through
			return nil
		}
		c.tick()
		return out
	}
	return msg
}

func (c *wheelCoalescer) tick() {
	time.AfterFunc(wheelFrame, func() { c.send(wheelFlushMsg{}) })
}

// take is the notches held, as one message, or nil when they cancel out.
func (c *wheelCoalescer) take() tea.Msg {
	n := c.net
	c.net = 0
	if n == 0 {
		return nil
	}
	w := c.pending
	w.Button = tea.MouseWheelDown
	if n < 0 {
		w.Button, n = tea.MouseWheelUp, -n
	}
	return wheelBatchMsg{wheel: w, n: n}
}

func wheelDir(msg tea.MouseWheelMsg) int {
	switch msg.Button {
	case tea.MouseWheelDown:
		return 1
	case tea.MouseWheelUp:
		return -1
	}
	return 0
}

func sameSpot(a, b tea.MouseWheelMsg) bool {
	return a.X == b.X && a.Y == b.Y && a.Mod == b.Mod
}

// wheelBatch runs a batch's notches through Update, drawing once after.
func (m Model) wheelBatch(b wheelBatchMsg) (tea.Model, tea.Cmd) {
	var out tea.Model = m
	cmds := make([]tea.Cmd, 0, b.n)
	for range b.n {
		var cmd tea.Cmd
		out, cmd = out.Update(b.wheel)
		cmds = append(cmds, cmd)
	}
	return out, tea.Batch(cmds...)
}
