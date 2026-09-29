package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/safeterm"
)

// A closed sprint, read only: ctrl+o lists the board's closed sprints, the
// last closed first. Picking one adds it as the last view, its lanes as they
// stood when it closed (the time machine, pinned there), the header saying
// what was done and what carried over. esc leaves it; so does another view.

// inClosedSprint is whether the board shows a closed sprint.
func (m *Model) inClosedSprint() bool {
	v, _ := m.jiraCurrentView()
	return !v.closed.IsZero()
}

// openClosedSprintPicker lists the board's closed sprints, fetched now.
func (m *Model) openClosedSprintPicker() tea.Cmd {
	t := m.jiraTab
	if t.cfg == nil || t.board >= len(t.boards) || t.boards[t.board].Type != "scrum" {
		m.status = "closed sprints are a scrum board's"
		return nil
	}
	gen := m.startJiraPicker(jiraPickClosedSprint, "Closed sprints", true)
	seq := m.jiraPicker.fetchSeq
	c, ctx, board := m.jiraClient, m.ctx, m.jiraBoardID()
	return func() tea.Msg {
		sprints, err := c.ClosedSprints(ctx, board)
		items := make([]jiraPickerItem, len(sprints))
		for i, s := range sprints {
			label := safeterm.Line(s.Name)
			if d := cmp.Or(s.Complete, s.End); !d.IsZero() {
				label += "  closed " + d.Local().Format("Jan 2 2006")
			}
			if g := safeterm.Line(strings.Join(strings.Fields(s.Goal), " ")); g != "" {
				label += "  · " + g
			}
			items[i] = jiraPickerItem{id: strconv.Itoa(s.ID), label: label}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickClosedSprint, items: items, err: err, sprints: sprints}
	}
}

// openClosedSprint shows the closed sprint with id as the last view, one
// closed sprint at a time.
func (m *Model) openClosedSprint(id string) tea.Cmd {
	t := m.jiraTab
	i := slices.IndexFunc(t.closedSprints, func(s jira.Sprint) bool { return strconv.Itoa(s.ID) == id })
	if i < 0 {
		return nil
	}
	s := t.closedSprints[i]
	v := jiraView{kind: jiraViewSprint, name: safeterm.Line(s.Name), sprint: s.ID, lanes: true, start: s.Start, end: s.End, goal: s.Goal,
		closed: cmp.Or(s.Complete, s.End, time.Now())}
	if n := len(t.views); n > 0 && !t.views[n-1].closed.IsZero() {
		t.views[n-1] = v
	} else {
		t.closedFrom = t.viewIdx
		t.views = append(t.views, v)
	}
	t.past = nil
	m.status = "reading " + v.name + "…"
	return m.loadJiraCards(len(t.views)-1, false)
}

// leaveClosedSprint goes back to the view the closed sprint was opened
// from; the closed one drops once its cards show (dropClosedSprint).
func (m *Model) leaveClosedSprint() tea.Cmd {
	t := m.jiraTab
	t.past = nil
	from := t.closedFrom
	if from < 0 || from >= len(t.views) || !t.views[from].closed.IsZero() {
		from = 0
	}
	m.status = ""
	return m.loadJiraCards(from, true)
}

// dropClosedSprint removes the closed sprint's view, and its pinned
// replay, once another view shows.
func (m *Model) dropClosedSprint() {
	t := m.jiraTab
	if m.inClosedSprint() {
		return
	}
	if p := t.past; p != nil && !p.at.IsZero() {
		t.past = nil
	}
	if n := len(t.views); n > 0 && !t.views[n-1].closed.IsZero() {
		t.views = t.views[:n-1]
	}
}

// closedSprintLine is what a closed sprint's cards came to: how many were
// done, and how many carried over into each sprint (or the backlog) they
// sit in now.
func closedSprintLine(cards []jira.Card) string {
	done, carried := 0, 0
	to := map[string]int{}
	for _, c := range cards {
		switch {
		case c.Sprint != "":
			to[c.Sprint]++
		case c.Done:
			done++
			continue
		default:
			to["backlog"]++
		}
		carried++
	}
	if carried == 0 {
		return fmt.Sprintf("%d done", done)
	}
	var parts []string
	for _, s := range slices.Sorted(maps.Keys(to)) {
		parts = append(parts, fmt.Sprintf("%d → %s", to[s], s))
	}
	return fmt.Sprintf("%d done · %d carried over: %s", done, carried, strings.Join(parts, ", "))
}
