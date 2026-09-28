package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// U opens the standup in place of the board, as W does the week: a table
// of issues in sections, the activity wrapped rather than cut, since it is
// read out in a meeting, often on a shared screen. tab switches between
// yours and the team's, [ ] step a workday, y copies it as text.

type standupState struct {
	team, byPerson bool
	since          time.Time
	head           string // the sprint goal and workdays left, for the team
	lines          []standupLine
	folded         []standupLine // Off the board, until z or enter shows it
	text           string
	row            int // the cursor, an index into lines
	top            int // the first screen line shown
	loading        bool
	err            string
	seq            int
}

// standupLine is a row of the standup: a section's heading, or an issue
// and its cells.
type standupLine struct {
	head   string // a section's name: the row is its heading
	unfold bool   // the heading of rows kept folded
	key    string
	title  string // key and summary, or "no ticket"
	who    string // the assignee, a status or a column
	age    string // how long in progress
	marks  string // flag, pull request, deploy
	what   string // what happened since, or no activity
}

// picks is whether the cursor stops on l.
func (l standupLine) picks() bool { return l.key != "" || l.unfold }

// text is the line as copied: its cells joined.
func (l standupLine) text() string {
	var parts []string
	for _, s := range []string{l.title, l.who, l.age, l.marks, l.what} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

type standupMsg struct {
	seq           int
	lines, folded []standupLine
	head, text    string
	err           error
}

// openStandup swaps the board for your standup since the previous workday.
func (m *Model) openStandup() tea.Cmd {
	m.jiraTab.standup = &standupState{since: jira.PreviousWorkday(time.Now(), m.opts.workdays)}
	m.focus = focusJira // from the panel too
	return m.loadStandup()
}

func (m *Model) loadStandup() tea.Cmd {
	s := m.jiraTab.standup
	s.seq++
	s.loading = true
	if s.team {
		return m.loadTeamStandup(s.seq, s.since, s.byPerson)
	}
	return m.loadMyStandup(s.seq, s.since)
}

func (m Model) handleStandup(msg standupMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.standup
	if s == nil || msg.seq != s.seq {
		return m, nil
	}
	s.loading = false
	if msg.err != nil {
		s.err = msg.err.Error()
		return m, nil
	}
	s.err, s.lines, s.folded, s.head, s.text = "", msg.lines, msg.folded, msg.head, msg.text
	s.row, s.top = 0, 0
	s.step(1) // onto the first issue
	return m, nil
}

// step moves the cursor to the next issue row d (1 or -1) away; it stays
// when there is none.
func (s *standupState) step(d int) {
	for i := s.row + d; i >= 0 && i < len(s.lines); i += d {
		if s.lines[i].picks() {
			s.row = i
			return
		}
	}
	if s.row < len(s.lines) && !s.lines[s.row].picks() && d > 0 {
		s.step(-1)
	}
}

// unfold puts the folded rows under their heading.
func (s *standupState) unfold() {
	i := slices.IndexFunc(s.lines, func(l standupLine) bool { return l.unfold })
	if i < 0 {
		return
	}
	s.lines[i].unfold = false
	s.lines = slices.Insert(s.lines, i+1, s.folded...)
	s.folded = nil
	s.row = i
	s.step(1)
}

func (m Model) handleStandupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.standup
	k := m.keys
	switch {
	case msg.String() == "ctrl+c":
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, k.Quit), key.Matches(msg, k.Standup):
		m.jiraTab.standup = nil
		m.renderJira()
	case key.Matches(msg, k.Up):
		s.step(-1)
	case key.Matches(msg, k.Down):
		s.step(1)
	case key.Matches(msg, k.Fold):
		s.unfold()
	case key.Matches(msg, k.OpenChannel):
		if s.row >= len(s.lines) {
			break
		}
		switch l := s.lines[s.row]; {
		case l.unfold:
			s.unfold()
		case l.key != "":
			return m.openJiraKey(l.key)
		}
	case key.Matches(msg, k.Tab):
		if !s.team && len(m.teamPeople()) == 0 {
			m.status = "no one is assigned a card on the board"
			break
		}
		s.team = !s.team
		return m, m.loadStandup()
	case key.Matches(msg, k.StandupGroup):
		if !s.team {
			s.team = true
		} else {
			s.byPerson = !s.byPerson
		}
		return m, m.loadStandup()
	case key.Matches(msg, k.PrevView):
		s.since = jira.PreviousWorkday(s.since, m.opts.workdays)
		return m, m.loadStandup()
	case key.Matches(msg, k.NextView):
		if !s.since.Before(jira.PreviousWorkday(time.Now(), m.opts.workdays)) {
			m.status = "the previous workday is the latest to start from"
			break
		}
		s.since = nextWorkday(s.since, m.opts.workdays)
		return m, m.loadStandup()
	case key.Matches(msg, k.Refresh):
		return m, m.loadStandup()
	case key.Matches(msg, k.CopyKey):
		if s.loading || s.err != "" || strings.TrimSpace(s.text) == "" {
			m.status = "nothing to copy yet"
			break
		}
		m.status = "standup copied"
		return m, tea.SetClipboard(s.text)
	case key.Matches(msg, k.Help):
		m.openHelp("Standup")
	}
	return m, nil
}

// standupViewLine is the view line: whose standup, since when, the goal
// and the keys.
func (m *Model) standupViewLine() string {
	s := m.jiraTab.standup
	now := time.Now()
	name := "Your standup"
	if s.team {
		name = "Team standup"
		if s.byPerson {
			name += " by person"
		}
	}
	line := jiraViewActive.Render(name) + jiraDimStyle.Render(" · since "+standupDay(s.since, now))
	if s.head != "" {
		line += jiraDimStyle.Render(" · " + s.head)
	}
	if s.loading {
		line += jiraDimStyle.Render(" · loading…")
	}
	k := m.keys
	other, group := "team", "by person"
	if s.team {
		other = "yours"
		if s.byPerson {
			group = "walk the board"
		}
	}
	keys := fmt.Sprintf("  ·  %s %s · %s %s · %s %s workday · %s copy · esc board",
		helpKey(k.Tab), other, helpKey(k.StandupGroup), group, helpKey(k.PrevView), helpKey(k.NextView), helpKey(k.CopyKey))
	return line + jiraDimStyle.Render(keys)
}

// renderStandup draws the standup's table into width × height: an issue
// per row, its activity wrapped in the last column.
func (m *Model) renderStandup(width, height int) string {
	s := m.jiraTab.standup
	switch {
	case s.err != "":
		out, _ := jiraErrorState(s.err, width, height, m.screenErrHints()...)
		return out
	case s.loading && s.lines == nil:
		return refDimStyle.Render("loading…")
	case len(s.lines) == 0:
		return refDimStyle.Render("nothing since " + standupDay(s.since, time.Now()))
	}
	// Columns as wide as their widest cell, capped; activity takes the rest.
	widest := func(cell func(standupLine) string, limit int) int {
		w := 0
		for _, l := range slices.Concat(s.lines, s.folded) {
			w = max(w, ansi.StringWidth(cell(l)))
		}
		return min(w, limit)
	}
	whoW := widest(func(l standupLine) string { return l.who }, 22)
	ageW := widest(func(l standupLine) string { return l.age }, 10)
	marksW := widest(func(l standupLine) string { return l.marks }, 26)
	fixed := 0
	for _, w := range []int{whoW, ageW, marksW} {
		if w > 0 {
			fixed += w + 2
		}
	}
	titleW := min(widest(func(l standupLine) string { return l.title }, 60), max((width-fixed)/2, 20))
	whatW := max(width-2-titleW-2-fixed, 16)
	pad := func(s string, w int) string {
		if w == 0 {
			return ""
		}
		s = ansi.Truncate(s, w, "…")
		return s + strings.Repeat(" ", w-ansi.StringWidth(s)) + "  "
	}

	var out []string
	cursor := 0 // the cursor row's first line
	for i, l := range s.lines {
		if l.head != "" {
			if len(out) > 0 {
				out = append(out, "")
			}
			head := l.head
			if l.unfold {
				head += "  " + helpKey(m.keys.Fold) + " shows them"
			}
			st := titleStyle
			if i == s.row {
				st, cursor = selectedRow, len(out)
			}
			out = append(out, st.Render(head))
			continue
		}
		what := cmp.Or(l.what, " ")
		wrapped := strings.Split(ansi.Wrap(what, whatW, " "), "\n")
		key, rest, _ := strings.Cut(l.title, " ")
		title := jiraKeyStyle.Render(key) + " " + rest
		if l.key == "" {
			title = l.title
		}
		first := "  " + pad(title, titleW) + pad(l.who, whoW) + pad(l.age, ageW) + pad(l.marks, marksW)
		indent := strings.Repeat(" ", ansi.StringWidth(ansi.Strip(first)))
		if i == s.row {
			cursor = len(out)
		}
		for j, w := range wrapped {
			line := indent + w
			if j == 0 {
				line = first + w
			}
			if l.what == "no activity" {
				line = strings.TrimSuffix(line, w) + jiraOverStyle.Render(w)
			}
			if strings.Contains(l.age, "stale") && j == 0 {
				line = strings.Replace(line, l.age, jiraOverStyle.Render(l.age), 1)
			}
			if i == s.row {
				line = selectedRow.Render(ansi.Strip(line) + strings.Repeat(" ", max(width-ansi.StringWidth(ansi.Strip(line)), 0)))
			}
			out = append(out, line)
		}
	}
	// Keep the cursor's row on screen, a line of what comes above it.
	if cursor < s.top+1 {
		s.top = max(cursor-1, 0)
	}
	if cursor >= s.top+height-2 {
		s.top = cursor - height + 3
	}
	s.top = min(max(s.top, 0), max(len(out)-height, 0))
	return strings.Join(out[s.top:min(len(out), s.top+height)], "\n")
}
