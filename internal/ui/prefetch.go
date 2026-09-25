package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Once the board's cursor rests, the issues under and around it are loaded
// into the client's cache, so opening one shows it at once.

const (
	prefetchDelay  = 300 * time.Millisecond
	prefetchAround = 2 // cards either side of the cursor
)

type prefetchMsg struct{ seq int }

// schedulePrefetch arms a prefetch for the cursor's cards once it rests.
func (m *Model) schedulePrefetch() tea.Cmd {
	m.prefetchSeq++
	seq := m.prefetchSeq
	return tea.Tick(prefetchDelay, func(time.Time) tea.Msg { return prefetchMsg{seq} })
}

func (m Model) handlePrefetch(msg prefetchMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.prefetchSeq {
		return m, nil
	}
	keys := m.jiraNeighbourKeys(prefetchAround)
	if len(keys) == 0 {
		return m, nil
	}
	c, ctx := m.jiraClient, m.ctx
	return m, func() tea.Msg {
		c.Prefetch(ctx, keys)
		return nil
	}
}

// jiraNeighbourKeys are the selected card and up to n either side, in its
// lane or the list, nearest first.
func (m *Model) jiraNeighbourKeys(n int) []string {
	t := m.jiraTab
	var idx []int // card indexes along the cursor's line
	pos := 0
	switch {
	case m.jiraShowsLanes():
		if t.lane >= len(t.lanes) {
			return nil
		}
		idx, pos = t.lanes[t.lane].cards, t.row
	default:
		idx, pos = t.order, t.idx
	}
	if pos >= len(idx) {
		return nil
	}
	keys := []string{t.cards[idx[pos]].Key}
	for d := 1; d <= n; d++ {
		for _, i := range []int{pos + d, pos - d} {
			if i >= 0 && i < len(idx) {
				keys = append(keys, t.cards[idx[i]].Key)
			}
		}
	}
	return keys
}
