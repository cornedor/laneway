package ui

import tea "charm.land/bubbletea/v2"

// . does the last change again, on the card under the cursor: a lane move
// or a quick or bulk edit (status, priority, assignee, sprint, labels,
// points). Faster than marks for a handful of cards.

// repeatAction is a change that can be made on another card.
type repeatAction struct {
	what  string
	apply func(m *Model, key string) tea.Cmd
}

func (m *Model) setRepeat(what string, apply func(m *Model, key string) tea.Cmd) {
	m.repeat = &repeatAction{what: what, apply: apply}
}

// repeatOnSelected makes the last change on the selected card.
func (m *Model) repeatOnSelected() tea.Cmd {
	c, ok := m.selectedJiraCard()
	switch {
	case m.repeat == nil:
		m.status = "nothing to repeat yet"
		return nil
	case !ok:
		m.status = "no card selected"
		return nil
	}
	m.status = "repeating " + m.repeat.what + " on " + c.Key + "…"
	return m.repeat.apply(m, c.Key)
}
