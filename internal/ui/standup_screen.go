package ui

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/standup"
)

// U opens the standup in place of the board, as W does the week. A strip
// on top goes round the people (← →): Everyone first, the board walked
// right to left, then each person's cards and what they did, the people
// already heard ticked. A picks who takes part, kept per board: on a big
// project the board's assignees are more than the team. With
// ui.standup_timer on, beside it a timer: each person's turn counting down
// (ui.standup_length split over them, or ui.standup_timebox), red when it
// runs out, and the whole standup's time; space pauses it. Below it the
// stop's table, its activity wrapped rather than cut, since it is read out
// in a meeting, often on a shared screen. [ ] step a workday, y copies the
// stop as text, P parks a card in a parking lot kept per sprint, which
// Everyone shows last.

type standupState struct {
	since time.Time
	head  string          // the sprint goal and workdays left
	board []standup.Stop  // everyone, then the people in the board's order
	stops []standup.Stop  // the same in the order gone round
	at    int             // the stop shown, an index into stops
	heard map[string]bool // the people already heard, by account
	in    []string        // who takes part, by account; none: everyone
	shuf  bool

	lines   []standup.Row // the stop's rows, and the parking lot on Everyone's
	folded  []standup.Row // Off the board, until z or enter shows it
	row     int           // the cursor, an index into lines
	top     int           // the first screen line shown
	loading bool
	err     string
	seq     int

	// The timer starts with the first person's turn: the standup's start,
	// the turn's (zero on Everyone), when it was paused (zero running).
	started, turn, paused time.Time
	tick                  int // the running tick's number, so one runs
}

type standupMsg struct {
	seq   int
	stops []standup.Stop
	head  string
	err   error
}

type standupTickMsg struct{ n int }

// openStandup swaps the board for the standup since ui.standup_lookback
// workdays back.
func (m *Model) openStandup() tea.Cmd {
	set := m.opts.standup
	m.jiraTab.standup = &standupState{since: standup.Since(time.Now(), m.opts.workdays, set.Lookback), heard: map[string]bool{}, shuf: set.Shuffle, in: m.standupPeople()}
	m.focus = focusJira // from the panel too
	return m.loadStandup()
}

func (m *Model) loadStandup() tea.Cmd {
	s := m.jiraTab.standup
	s.seq++
	s.loading = true
	return m.loadTeamStandup(s.seq, s.since)
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
	first := s.board == nil
	who := s.person().ID
	s.err, s.board, s.head = "", msg.stops, msg.head
	s.order(who, false)
	if first && m.opts.standup.First && len(s.stops) > 1 {
		return m, m.standupGo(1)
	}
	m.showStop()
	return m, nil
}

// person is the shown stop's person, the zero one on Everyone.
func (s *standupState) person() standup.Person {
	if s.at < len(s.stops) {
		return s.stops[s.at].Person
	}
	return standup.Person{}
}

// order lays the stops out, Everyone first and then those taking part:
// the board's order, or shuffled (again when reshuffle, else as gone round
// so far, so a reload keeps it), and stays on who.
func (s *standupState) order(who string, reshuffle bool) {
	prev := s.stops
	s.stops = slices.DeleteFunc(slices.Clone(s.board), func(st standup.Stop) bool {
		return st.Person.ID != "" && len(s.in) > 0 && !slices.Contains(s.in, st.Person.ID)
	})
	if s.shuf && len(s.stops) > 2 {
		people := s.stops[1:]
		rand.Shuffle(len(people), func(i, j int) { people[i], people[j] = people[j], people[i] })
		if !reshuffle && len(prev) > 0 {
			was := func(st standup.Stop) int {
				i := slices.IndexFunc(prev, func(p standup.Stop) bool { return p.Person.ID == st.Person.ID })
				return cmp.Or(i+1, len(prev)+1) // someone new goes last
			}
			slices.SortStableFunc(people, func(a, b standup.Stop) int { return was(a) - was(b) })
		}
	}
	s.at = max(slices.IndexFunc(s.stops, func(st standup.Stop) bool { return st.Person.ID == who }), 0)
}

// showStop puts the shown stop's rows on screen, the cursor on its first
// card.
func (m *Model) showStop() {
	s := m.jiraTab.standup
	s.lines, s.folded, s.row, s.top = nil, nil, 0, 0
	if s.at < len(s.stops) {
		st := s.stops[s.at]
		s.lines, s.folded = slices.Clone(st.Rows), slices.Clone(st.Folded)
	}
	s.parkLot(m.parkedKeys())
	s.step(1)
}

// standupGo moves d stops round, marking the person left heard; a turn
// starts on a person's stop, the timer with the first.
func (m *Model) standupGo(d int) tea.Cmd {
	s := m.jiraTab.standup
	if len(s.stops) < 2 {
		m.status = "no one is assigned a card on the board"
		return nil
	}
	if p := s.person(); p.ID != "" {
		s.heard[p.ID] = true
	}
	s.at = (s.at + d + len(s.stops)) % len(s.stops)
	m.showStop()
	now := time.Now()
	s.turn = time.Time{}
	if s.at == 0 {
		return nil
	}
	s.turn = now
	if !s.paused.IsZero() {
		s.turn = s.paused // a full turn once it runs again
	}
	if s.started.IsZero() && m.opts.standup.Timer {
		s.started = now
		return s.startTick()
	}
	return nil
}

// startTick runs the timer's tick, stopping one already running.
func (s *standupState) startTick() tea.Cmd {
	s.tick++
	n := s.tick
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return standupTickMsg{n} })
}

func (m Model) handleStandupTick(msg standupTickMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.standup
	if s == nil || msg.n != s.tick || !s.paused.IsZero() {
		return m, nil
	}
	return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return standupTickMsg{msg.n} })
}

// pause stops the timer, or runs it again with the pause left out; before
// the first turn it starts the standup's time.
func (s *standupState) pause(now time.Time) (string, tea.Cmd) {
	switch {
	case s.started.IsZero():
		s.started = now
		return "the standup's timer runs", s.startTick()
	case s.paused.IsZero():
		s.paused = now
		return "the timer is paused", nil
	}
	away := now.Sub(s.paused)
	s.started = s.started.Add(away)
	if !s.turn.IsZero() {
		s.turn = s.turn.Add(away)
	}
	s.paused = time.Time{}
	return "the timer runs again", s.startTick()
}

// clock reads the timer at now: the standup's time, and on a person's
// stop what is left of the turn (below zero when over).
func (s *standupState) clock(now time.Time, turn time.Duration) (total, left time.Duration, inTurn bool) {
	if s.started.IsZero() {
		return 0, 0, false
	}
	if !s.paused.IsZero() {
		now = s.paused
	}
	total = now.Sub(s.started)
	if s.turn.IsZero() || turn == 0 {
		return total, 0, false
	}
	return total, turn - now.Sub(s.turn), true
}

// standupPeopleMeta keeps who takes part per board.
func (m *Model) standupPeopleMeta() string {
	return jiraMetaPrefix + "standup:people:" + strconv.Itoa(m.jiraBoardID())
}

func (m *Model) standupPeople() []string {
	if m.store == nil {
		return nil
	}
	raw, _, _ := m.store.GetMeta(m.standupPeopleMeta())
	return strings.Fields(raw)
}

// openStandupPeople ticks who takes part: none ticked is everyone.
func (m *Model) openStandupPeople() {
	s := m.jiraTab.standup
	if len(s.board) < 2 {
		m.status = "no one is assigned a card on the board"
		return
	}
	m.startJiraPicker(jiraPickStandupPeople, "Who takes part", true)
	m.jiraPicker.checked = map[string]string{}
	items := []jiraPickerItem{{id: "", label: "Everyone on the board"}}
	for _, st := range s.board[1:] {
		items = append(items, jiraPickerItem{id: st.Person.ID, label: st.Person.Name})
		if slices.Contains(s.in, st.Person.ID) {
			m.jiraPicker.checked[st.Person.ID] = st.Person.Name
		}
	}
	m.setJiraPickerItems(items)
	m.markChecked()
}

// setStandupPeople keeps ids as who takes part ("" alone: everyone) and
// goes round them; the timer splits the length over them.
func (m *Model) setStandupPeople(ids []string) {
	s := m.jiraTab.standup
	if s == nil {
		return
	}
	s.in = slices.DeleteFunc(ids, func(id string) bool { return id == "" })
	if m.store != nil {
		_ = m.store.SetMeta(m.standupPeopleMeta(), strings.Join(s.in, " "))
	}
	s.order(s.person().ID, false)
	m.showStop()
	m.status = "everyone on the board takes part"
	switch n := len(s.stops) - 1; {
	case len(s.in) == 0:
	case n == 1:
		m.status = "1 person takes part"
	default:
		m.status = strconv.Itoa(n) + " people take part"
	}
}

// parkHead heads the parking lot, Everyone's last section.
const parkHead = "Parking lot"

// parkMeta keeps the parked keys per board and sprint (or view).
func (m *Model) parkMeta() string {
	key := jiraMetaPrefix + "park:" + strconv.Itoa(m.jiraBoardID()) + ":"
	if v, ok := m.jiraCurrentView(); ok {
		if v.sprint != 0 {
			return key + strconv.Itoa(v.sprint)
		}
		return key + v.name
	}
	return key
}

func (m *Model) parkedKeys() []string {
	if m.store == nil {
		return nil
	}
	raw, _, _ := m.store.GetMeta(m.parkMeta())
	return strings.Fields(raw)
}

// togglePark parks key, or takes it out of the parking lot.
func (m *Model) togglePark(key string) {
	parked := m.parkedKeys()
	if i := slices.Index(parked, key); i >= 0 {
		parked = slices.Delete(parked, i, i+1)
		m.status = key + " out of the parking lot"
	} else {
		parked = append(parked, key)
		m.status = key + " parked for after the standup"
	}
	if m.store != nil {
		_ = m.store.SetMeta(m.parkMeta(), strings.Join(parked, " "))
	}
	m.jiraTab.standup.parkLot(parked)
}

// parkLot marks the parked rows and, on Everyone's stop, puts them again
// in the parking lot after the rest; a parked card no longer on the
// standup shows by its key.
func (s *standupState) parkLot(parked []string) {
	if i := slices.IndexFunc(s.lines, func(l standup.Row) bool { return strings.HasPrefix(l.Head, parkHead) }); i >= 0 {
		s.lines = s.lines[:i]
	}
	mark := func(l *standup.Row, on bool) {
		ms := slices.DeleteFunc(strings.Split(l.Marks, " · "), func(x string) bool { return x == "" || x == "parked" })
		if on {
			ms = append(ms, "parked")
		}
		l.Marks = strings.Join(ms, " · ")
	}
	for _, rows := range [][]standup.Row{s.lines, s.folded} {
		for i := range rows {
			if rows[i].Key != "" {
				mark(&rows[i], slices.Contains(parked, rows[i].Key))
			}
		}
	}
	if s.at != 0 || len(s.stops) == 0 {
		s.row = min(s.row, max(len(s.lines)-1, 0))
		return
	}
	var lot []standup.Row
	all := slices.Concat(s.stops[0].Rows, s.stops[0].Folded)
	for _, k := range parked {
		l := standup.Row{Key: k, Title: k}
		if i := slices.IndexFunc(all, func(l standup.Row) bool { return l.Key == k }); i >= 0 {
			l = all[i]
		}
		mark(&l, false)
		lot = append(lot, l)
	}
	if len(lot) > 0 {
		s.lines = append(s.lines, standup.Row{Head: fmt.Sprintf("%s (%d)", parkHead, len(lot))})
		s.lines = append(s.lines, lot...)
	}
	s.row = min(s.row, max(len(s.lines)-1, 0))
}

// copyText is the shown stop as copied: whose it is, its text, then the
// parking lot.
func (s *standupState) copyText() string {
	if s.at >= len(s.stops) {
		return ""
	}
	st := s.stops[s.at]
	text := st.Text
	if st.Person.ID != "" {
		text = st.Person.Name + "\n\n" + text
	} else if s.head != "" {
		text = s.head + "\n\n" + text
	}
	for i, l := range s.lines {
		if strings.HasPrefix(l.Head, parkHead) {
			text += "\n\n" + parkHead + "\n"
			for _, p := range s.lines[i+1:] {
				text += "- " + p.Text() + "\n"
			}
			text = strings.TrimSuffix(text, "\n")
		}
	}
	return text
}

// picks is whether the cursor stops on l.
func picks(l standup.Row) bool { return l.Key != "" || l.Unfold }

// step moves the cursor to the next issue row d (1 or -1) away; it stays
// when there is none.
func (s *standupState) step(d int) {
	for i := s.row + d; i >= 0 && i < len(s.lines); i += d {
		if picks(s.lines[i]) {
			s.row = i
			return
		}
	}
	if s.row < len(s.lines) && !picks(s.lines[s.row]) && d > 0 {
		s.step(-1)
	}
}

// unfold puts the folded rows under their heading.
func (s *standupState) unfold() {
	i := slices.IndexFunc(s.lines, func(l standup.Row) bool { return l.Unfold })
	if i < 0 {
		return
	}
	s.lines[i].Unfold = false
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
	case key.Matches(msg, k.Left):
		return m, m.standupGo(-1)
	case key.Matches(msg, k.Right):
		return m, m.standupGo(1)
	case key.Matches(msg, k.Fold):
		s.unfold()
	case key.Matches(msg, k.StandupPause):
		if !m.opts.standup.Timer {
			m.status = "no timer: ui.standup_timer is off"
			break
		}
		var cmd tea.Cmd
		m.status, cmd = s.pause(time.Now())
		return m, cmd
	case key.Matches(msg, k.Assignee):
		m.openStandupPeople()
	case key.Matches(msg, k.StandupShuffle):
		s.shuf = !s.shuf
		s.order(s.person().ID, true)
		m.status = "the board's order"
		if s.shuf {
			m.status = "a random order"
		}
	case key.Matches(msg, k.StandupPark):
		if s.row >= len(s.lines) || s.lines[s.row].Key == "" {
			m.status = "no card to park"
			break
		}
		m.togglePark(s.lines[s.row].Key)
	case key.Matches(msg, k.OpenChannel):
		if s.row >= len(s.lines) {
			break
		}
		switch l := s.lines[s.row]; {
		case l.Unfold:
			s.unfold()
		case l.Key != "":
			return m.openJiraKey(l.Key)
		}
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
		if s.loading || s.err != "" || strings.TrimSpace(s.copyText()) == "" {
			m.status = "nothing to copy yet"
			break
		}
		m.status = "standup copied"
		return m, tea.SetClipboard(s.copyText())
	case key.Matches(msg, k.Help):
		m.openHelp("Standup")
	}
	return m, nil
}

// standupViewLine is the view line: since when, the goal and the keys.
func (m *Model) standupViewLine() string {
	s := m.jiraTab.standup
	line := jiraViewActive.Render("Standup") + jiraDimStyle.Render(" · since "+standup.Day(s.since, time.Now()))
	if s.head != "" {
		line += jiraDimStyle.Render(" · " + s.head)
	}
	if s.loading {
		line += jiraDimStyle.Render(" · loading…")
	}
	k := m.keys
	order := "shuffle"
	if s.shuf {
		order = "board order"
	}
	timer := ""
	if m.opts.standup.Timer {
		timer = helpKey(k.StandupPause) + " timer · "
	}
	keys := fmt.Sprintf("  ·  ← → person · %s who's in · %s%s %s · %s park · %s %s workday · %s copy · esc board",
		helpKey(k.Assignee), timer, helpKey(k.StandupShuffle), order, helpKey(k.StandupPark),
		helpKey(k.PrevView), helpKey(k.NextView), helpKey(k.CopyKey))
	return line + jiraDimStyle.Render(keys)
}

// standupStrip is the line under it, width wide: the people to go round,
// then the one shown and the timer, right-aligned. Nothing changes width
// with what it shows, so stepping round moves nothing: each chip is the
// same size (brackets mark the one shown, a tick those heard) and the
// timer's fields are padded.
func (m *Model) standupStrip(now time.Time, width int) string {
	s := m.jiraTab.standup
	if len(s.stops) == 0 {
		return ""
	}
	parts := []string{jiraDimStyle.Render("‹")}
	for i, st := range s.stops {
		p := st.Person
		body, open, shut, tick := "Everyone", " ", " ", " "
		if p.ID != "" {
			body = jiraAvatar(p.Name)
		}
		if i == s.at {
			open, shut = jiraViewActive.Render("["), jiraViewActive.Render("]")
		}
		if s.heard[p.ID] {
			tick = jiraDimStyle.Render("✓")
		}
		parts = append(parts, open+body+shut+tick)
	}
	parts = append(parts, jiraDimStyle.Render("›"))
	st := s.stops[min(s.at, len(s.stops)-1)]
	who := titleStyle.Render("Everyone") + jiraDimStyle.Render(" · the board")
	if st.Person.ID != "" {
		who = titleStyle.Render(st.Person.Name) + jiraDimStyle.Render(fmt.Sprintf(" · %d of %d", s.at, len(s.stops)-1))
		if st.Quiet {
			who += jiraDimStyle.Render(" · no changes")
		}
	}
	if len(s.in) > 0 {
		who += jiraDimStyle.Render(fmt.Sprintf(" · %d of %d in", len(s.stops)-1, len(s.board)-1))
	}
	left := strings.Join(parts, "") + "  " + who
	if !m.opts.standup.Timer {
		return ansi.Truncate(left, width, "…")
	}

	total, rest, inTurn := s.clock(now, m.opts.standup.Turn(len(s.stops)-1))
	turn := jiraDimStyle.Render(fmt.Sprintf("%6s left", standupClock(m.opts.standup.Turn(len(s.stops)-1)))) // a turn's length till one runs
	switch {
	case inTurn && rest < 0:
		turn = fmt.Sprintf("%6s over", "+"+standupClock(-rest))
		if now.Second()%2 == 0 || !s.paused.IsZero() {
			turn = jiraOverStyle.Render(turn)
		} else {
			turn = jiraOverStyle.Reverse(true).Render(turn)
		}
	case inTurn:
		turn = titleStyle.Render(fmt.Sprintf("%6s", standupClock(rest+time.Second-1))) + jiraDimStyle.Render(" left") // counting down, a second shows till it is gone
	}
	of := fmt.Sprintf("%5s of %5s", standupClock(total), standupClock(m.opts.standup.Length))
	if total > m.opts.standup.Length {
		of = jiraOverStyle.Render(of)
	} else {
		of = jiraDimStyle.Render(of)
	}
	starts := helpKey(m.keys.StandupPause) + " starts"
	state := ""
	switch {
	case s.started.IsZero():
		state = starts
	case !s.paused.IsZero():
		state = "paused"
	}
	state = jiraDimStyle.Render(fmt.Sprintf("%-*s", max(ansi.StringWidth(starts), len("paused")), state))
	timer := "⏱ " + turn + jiraDimStyle.Render(" · ") + of + "  " + state
	room := width - ansi.StringWidth(timer) - 2
	if room < 1 {
		return ansi.Truncate(left, width, "…")
	}
	left = ansi.Truncate(left, room, "…")
	return left + strings.Repeat(" ", width-ansi.StringWidth(left)-ansi.StringWidth(timer)) + timer
}

// standupClock is d as m:ss.
func standupClock(d time.Duration) string {
	d = d.Truncate(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// renderStandup draws the stop's table into width × height: an issue per
// row, its activity wrapped in the last column.
func (m *Model) renderStandup(width, height int) string {
	s := m.jiraTab.standup
	switch {
	case s.err != "":
		out, _ := jiraErrorState(s.err, width, height, m.screenErrHints()...)
		return out
	case s.loading && s.stops == nil:
		return refDimStyle.Render("loading…")
	case len(s.lines) == 0:
		return refDimStyle.Render("no changes since " + standup.Day(s.since, time.Now()))
	}
	// Columns as wide as their widest cell, capped; activity takes the rest.
	widest := func(cell func(standup.Row) string, limit int) int {
		w := 0
		for _, l := range slices.Concat(s.lines, s.folded) {
			w = max(w, ansi.StringWidth(cell(l)))
		}
		return min(w, limit)
	}
	whoW := widest(func(l standup.Row) string { return l.Who }, 22)
	ageW := widest(func(l standup.Row) string { return l.Age }, 10)
	marksW := widest(func(l standup.Row) string { return l.Marks }, 26)
	fixed := 0
	for _, w := range []int{whoW, ageW, marksW} {
		if w > 0 {
			fixed += w + 2
		}
	}
	titleW := min(widest(func(l standup.Row) string { return l.Title }, 60), max((width-fixed)/2, 20))
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
		if l.Head != "" {
			if len(out) > 0 {
				out = append(out, "")
			}
			head := l.Head
			if l.Unfold {
				head += "  " + helpKey(m.keys.Fold) + " shows them"
			}
			st := titleStyle
			if i == s.row {
				st, cursor = selectedRow, len(out)
			}
			out = append(out, st.Render(head))
			continue
		}
		what := cmp.Or(l.What, " ")
		wrapped := strings.Split(ansi.Wrap(what, whatW, " "), "\n")
		key, rest, _ := strings.Cut(l.Title, " ")
		title := jiraKeyStyle.Render(key) + " " + rest
		if l.Key == "" {
			title = l.Title
		}
		first := "  " + pad(title, titleW) + pad(l.Who, whoW) + pad(l.Age, ageW) + pad(l.Marks, marksW)
		indent := strings.Repeat(" ", ansi.StringWidth(ansi.Strip(first)))
		if i == s.row {
			cursor = len(out)
		}
		for j, w := range wrapped {
			line := indent + w
			if j == 0 {
				line = first + w
			}
			if l.What == "no activity" {
				line = strings.TrimSuffix(line, w) + jiraOverStyle.Render(w)
			}
			if strings.Contains(l.Age, "stale") && j == 0 {
				line = strings.Replace(line, l.Age, jiraOverStyle.Render(l.Age), 1)
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

// nextWorkday is the first workday after day.
func nextWorkday(day time.Time, workdays []time.Weekday) time.Time {
	if len(workdays) == 0 {
		workdays = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	}
	d := day.AddDate(0, 0, 1)
	for !slices.Contains(workdays, d.Weekday()) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}
