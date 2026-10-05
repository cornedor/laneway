package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/review"
)

// ctrl+r shows the issues of the pull and merge requests waiting on your
// review as a view: gh's and glab's, the keys found in their titles and
// branches. Their cards get a ⌥ for the rest of the session.

// reviewMsg is the keys waiting on your review.
type reviewMsg struct {
	keys []string
	err  error
}

// openReview asks gh and glab, then shows the keys found as a view.
func (m *Model) openReview() tea.Cmd {
	if m.jiraTab.cfg == nil {
		m.status = "open a board first"
		return nil
	}
	if m.demo { // gh and glab ask the user's own forges
		return func() tea.Msg { return reviewMsg{} }
	}
	m.status = "asking gh and glab what waits on your review…"
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		reqs, err := review.Requests(ctx)
		if err != nil {
			return reviewMsg{err: err}
		}
		projects, err := c.ListProjects(ctx)
		return reviewMsg{keys: review.Keys(reqs, projects), err: err}
	}
}

func (m Model) handleReview(msg reviewMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail("review requests: " + msg.err.Error())
		return m, nil
	}
	m.reviewKeys = map[string]bool{}
	for _, k := range msg.keys {
		m.reviewKeys[k] = true
	}
	if len(msg.keys) == 0 {
		m.status = "nothing waits on your review (no issue keys in the requests' titles or branches)"
		return m, nil
	}
	m.status = ""
	return m, m.runNamedJQLView("Review: waiting on me", "key in ("+strings.Join(msg.keys, ", ")+") ORDER BY updated DESC")
}
