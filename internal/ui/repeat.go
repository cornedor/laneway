package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
)

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
		m.status = i18n.T("nothing to repeat yet")
		return nil
	case !ok:
		m.status = i18n.T("no card selected")
		return nil
	}
	m.status = i18n.Tf("repeating %s on %s…", m.repeat.what, c.Key)
	return m.repeat.apply(m, c.Key)
}
