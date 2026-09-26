package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The board's header rows as segments: what each draws and what a click on
// it does, so drawing and the mouse agree.

// headSeg is one run of a header row.
type headSeg struct {
	s     string // as drawn
	kind  string // a click: "view", "quick", "term", "chart", "assignee", "esc", "key", "" nothing
	i     int    // the view, quick filter, term or chart
	press string // the key a "key" click presses
}

func plainSeg(s string) headSeg { return headSeg{s: s} }

// keySeg presses b's first key; pressSeg presses k.
func keySeg(s string, b key.Binding) headSeg {
	if len(b.Keys()) == 0 {
		return plainSeg(s)
	}
	return pressSeg(s, firstKey(b))
}

func pressSeg(s, k string) headSeg { return headSeg{s: s, kind: "key", press: k} }

func joinSegs(segs []headSeg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.s)
	}
	return b.String()
}

// jiraTitleSegs is the title row: project, board, freshness, timer, inbox
// and the main keys, each a click on its key.
func (m *Model) jiraTitleSegs() []headSeg {
	t, k := m.jiraTab, m.keys
	dim := refDimStyle.Render
	segs := []headSeg{plainSeg(titleStyle.Render("Jira"))}
	if t.project != "" {
		segs = append(segs, plainSeg(" "), keySeg(titleStyle.Render(t.project), k.Project))
	}
	if t.board < len(t.boards) {
		segs = append(segs, plainSeg(dim("  ")), keySeg(dim(t.boards[t.board].Name), k.Board))
	}
	switch {
	case t.loading:
		segs = append(segs, plainSeg(dim("  refreshing…")))
	case !t.fetched.IsZero():
		segs = append(segs, plainSeg(dim("  ")), keySeg(dim("updated "+age(t.fetched)), k.Refresh))
		if t.total > len(t.cards) {
			segs = append(segs, plainSeg(dim(fmt.Sprintf("  ·  first %d of %d", len(t.cards), t.total))))
		}
	}
	if tl := m.timerLabel(); tl != "" {
		segs = append(segs, plainSeg(dim("  ·  ")), keySeg(dim(tl), k.Timer))
	}
	if b := m.inboxBadge(); b != "" {
		segs = append(segs, plainSeg(dim("  ·  ")), keySeg(dim(b+" "+helpKey(k.Inbox)), k.Inbox))
	}
	segs = append(segs, plainSeg(dim("  ·  ")))
	for i, h := range []struct {
		b    key.Binding
		what string
	}{{k.Help, "help"}, {k.Project, "project"}, {k.Board, "board"}} {
		if i > 0 {
			segs = append(segs, plainSeg(dim("  ")))
		}
		segs = append(segs, keySeg(dim(helpKey(h.b)+" "+h.what), h.b))
	}
	segs = append(segs, plainSeg(dim("  ")), keySeg(dim(helpKey(k.PrevView)), k.PrevView), plainSeg(dim(" ")),
		keySeg(dim(helpKey(k.NextView)), k.NextView), plainSeg(dim(" view")))
	for _, h := range []struct {
		b    key.Binding
		what string
	}{{k.ToggleMode, "lanes/list"}, {k.OpenChannel, "open"}, {k.OpenAttach, "browser"}, {k.Refresh, "refresh"}} {
		segs = append(segs, plainSeg(dim("  ")), keySeg(dim(helpKey(h.b)+" "+h.what), h.b))
	}
	if m.jiraShowsLanes() {
		segs = append(segs, plainSeg(dim("  "+helpKey(k.MoveCardLeft)+"/"+helpKey(k.MoveCardRight)+" move")))
	}
	return segs
}

// jiraViewSegs is the views row: the offline notice (retries), the views
// (‹ the one before the first shown), the sprint's bar (charts), its days
// and which lanes show.
func (m *Model) jiraViewSegs() []headSeg {
	t := m.jiraTab
	var segs []headSeg
	if t.offline != "" {
		segs = append(segs, keySeg(jiraOverStyle.Render("offline · showing the cached board · "+helpKey(m.keys.Refresh)+" retries"), m.keys.Refresh), plainSeg("    "))
	}
	first := min(t.viewsFirst, len(t.views))
	if first > 0 {
		segs = append(segs, headSeg{s: jiraDimStyle.Render("‹"), kind: "view", i: first - 1}, plainSeg(jiraDimStyle.Render(jiraViewSep)))
	}
	for i, v := range t.views[first:] {
		if i > 0 {
			segs = append(segs, plainSeg(jiraDimStyle.Render(jiraViewSep)))
		}
		style := jiraDimStyle
		if first+i == t.viewIdx {
			style = jiraViewActive
		}
		segs = append(segs, headSeg{s: style.Render(v.name), kind: "view", i: first + i})
	}
	if v, ok := m.jiraCurrentView(); ok {
		if bar := jiraSprintBar(t.cards); v.kind == jiraViewSprint && bar != "" {
			segs = append(segs, plainSeg("    "), keySeg(bar, m.keys.Charts))
		}
		if s := jiraSprintLine(v, time.Now(), m.opts.workdays); s != "" {
			segs = append(segs, plainSeg(jiraDimStyle.Render("    "+s)))
		}
	}
	if n := len(t.lanes); m.jiraShowsLanes() && n > 0 {
		if vis, _ := jiraLaneLayout(t.view.Width(), n); vis < n {
			segs = append(segs, plainSeg(jiraDimStyle.Render(fmt.Sprintf("    lanes %d–%d of %d", t.firstLane+1, t.firstLane+vis, n))))
		}
	}
	return segs
}

// jiraFilterSegs is the filters row: the search (its terms remove one, esc
// all), the assignee, the quick filters, and the clear, sort and search keys.
func (m *Model) jiraFilterSegs() []headSeg {
	t := m.jiraTab
	dim := jiraDimStyle.Render
	chip := func(on bool, s string) string {
		if on {
			return jiraViewActive.Render(s)
		}
		return dim(s)
	}
	var segs []headSeg
	switch {
	case t.searching:
		segs = append(segs, plainSeg(t.search.View()), plainSeg("  "))
	case t.jiraSearchQuery() != "":
		segs = append(segs, plainSeg(dim("/")))
		for i, w := range jiraQueryWords(t.search.Value()) {
			if i > 0 {
				segs = append(segs, plainSeg(" "))
			}
			segs = append(segs, headSeg{s: chip(true, w+" ×"), kind: "term", i: i})
		}
		segs = append(segs, plainSeg(" "), headSeg{s: dim("esc"), kind: "esc"}, plainSeg("  "))
	}
	who := "everyone"
	if t.assignee.id != "" {
		who = t.assignee.label
	}
	segs = append(segs, plainSeg(dim(helpKey(m.keys.Assignee)+" assignee ("+helpKey(m.keys.Mine)+" me): ")),
		headSeg{s: chip(t.assignee.id != "", who), kind: "assignee"})
	for i, q := range t.quick {
		if i == 9 {
			break
		}
		segs = append(segs, plainSeg("  "), headSeg{s: chip(t.quickOn[q.ID], strconv.Itoa(i+1)+" "+q.Name), kind: "quick", i: i})
	}
	if t.jiraFiltered() {
		segs = append(segs, plainSeg(dim("  ·  ")), keySeg(dim(helpKey(m.keys.ClearFilters)+" clears"), m.keys.ClearFilters))
	}
	if t.sort != jiraSortRank && !m.jiraShowsLanes() {
		segs = append(segs, plainSeg(dim("  ·  ")), keySeg(dim(helpKey(m.keys.Sort)+" sort: ")+chip(true, t.sort.String()), m.keys.Sort))
	}
	if !t.searching && t.jiraSearchQuery() == "" {
		segs = append(segs, plainSeg(dim("  ·  ")), keySeg(dim(helpKey(m.keys.Search)+" search"), m.keys.Search))
	}
	return segs
}

// headerHit is the header segment at x, y; its kind is "" for nothing.
func (m *Model) headerHit(x, y int) headSeg {
	t := m.jiraTab
	other := t.roadmap != nil || t.plan != nil || t.charts != nil
	var segs []headSeg
	switch {
	case y == 0 && !other:
		segs = m.jiraTitleSegs()
	case y == jiraBodyTop-2 && t.roadmap != nil:
		segs = m.roadmapSegs()
	case y == jiraBodyTop-2 && t.charts != nil:
		segs = m.chartsSegs()
	case y == jiraBodyTop-2 && t.plan != nil:
		segs = m.planSegs()
	case y == jiraBodyTop-2:
		segs = m.jiraViewSegs()
	case y == jiraBodyTop-1 && !other:
		if t.searching {
			return headSeg{}
		}
		segs = m.jiraFilterSegs()
	}
	return segAt(segs, x, 1) // after the box's left border
}

// clickHeader acts on a header hit; ok false when x, y is none.
func (m Model) clickHeader(x, y int) (tea.Model, tea.Cmd, bool) {
	return m.runSeg(m.headerHit(x, y))
}

// runSeg does what a click on segment h does; ok false when nothing.
func (m Model) runSeg(h headSeg) (tea.Model, tea.Cmd, bool) {
	switch h.kind {
	case "view":
		return m, m.cycleJiraView(h.i - m.jiraTab.viewIdx), true
	case "quick":
		return m, m.toggleJiraQuick(h.i), true
	case "term":
		m.removeSearchTerm(h.i)
		return m, nil, true
	case "chart":
		m.jiraTab.charts.tab = h.i
		return m, nil, true
	case "esc":
		m.clearJiraSearch()
		return m, nil, true
	case "assignee":
		h = keySeg("", m.keys.Assignee)
	case "key":
	default:
		return m, nil, false
	}
	if h.press == "" {
		return m, nil, true
	}
	out, cmd := m.handleJiraKey(keyPress(h.press))
	return out, cmd, true
}

// segAt is the segment of segs at column x, the row starting at column at.
func segAt(segs []headSeg, x, at int) headSeg {
	for _, s := range segs {
		w := ansi.StringWidth(s.s)
		if x >= at && x < at+w {
			return s
		}
		at += w
	}
	return headSeg{}
}

// hint is a key shown as label that a click presses; a pair has two keys
// before one what ("← → scroll").
type hint struct{ label, press, label2, press2, what string }

func keyHint(b key.Binding, what string) hint {
	return hint{label: helpKey(b), press: firstKey(b), what: what}
}

func firstKey(b key.Binding) string {
	if len(b.Keys()) == 0 {
		return ""
	}
	return b.Keys()[0]
}

// hintSegs lays hints out two cells apart, each key pressing itself.
func hintSegs(hints ...hint) []headSeg {
	dim := jiraDimStyle.Render
	var segs []headSeg
	for _, h := range hints {
		segs = append(segs, plainSeg(dim("  ")), pressSeg(dim(h.label), h.press))
		if h.label2 != "" {
			segs = append(segs, plainSeg(dim(" ")), pressSeg(dim(h.label2), h.press2))
		}
		segs = append(segs, pressSeg(dim(" "+h.what), h.press))
	}
	return segs
}

// roadmapSegs is the roadmap's view line: its keys press as hints.
func (m *Model) roadmapSegs() []headSeg {
	r := m.jiraTab.roadmap
	dim := jiraDimStyle.Render
	s := jiraViewActive.Render("Roadmap") + dim(fmt.Sprintf("  %d epics · %s per column", len(r.epics), roadmapZoomName(roadmapZooms[r.zoom])))
	if n := len(r.groups); n > 0 {
		s += dim(fmt.Sprintf(" · %d parents", n))
	}
	segs := []headSeg{plainSeg(s)}
	switch {
	case r.loading:
		segs = append(segs, plainSeg(dim("  ·  loading…")))
	case !r.fetched.IsZero():
		segs = append(segs, plainSeg(dim("  ·  ")), keySeg(dim("updated "+age(r.fetched)), m.keys.Refresh))
	}
	k := m.keys
	segs = append(segs, plainSeg(dim("  ·")))
	segs = append(segs, hintSegs(hint{"←", "left", "→", "right", "scroll"}, hint{"+", "+", "-", "-", "zoom"},
		hint{label: ".", press: ".", what: "today"}, hint{label: "space", press: "space", what: "children"})...)
	segs = append(segs, plainSeg(dim("  "+helpKey(k.MoveCardLeft)+"/"+helpKey(k.MoveCardRight)+" move  < > end  e grip an end")))
	return append(segs, hintSegs(keyHint(k.OpenChannel, "open"), keyHint(k.CopyKey, "copy"), hint{label: "esc", press: "esc", what: "board"})...)
}

// chartsSegs is the charts' view line: a chart's name shows it.
func (m *Model) chartsSegs() []headSeg {
	ch := m.jiraTab.charts
	var segs []headSeg
	for n, i := range ch.chartTabsShown() {
		if n > 0 {
			segs = append(segs, plainSeg(jiraDimStyle.Render(chartTabSep)))
		}
		style := jiraDimStyle
		if i == ch.tab {
			style = jiraViewActive
		}
		segs = append(segs, headSeg{s: style.Render(chartTabNames[i]), kind: "chart", i: i})
	}
	if ch.loading {
		segs = append(segs, plainSeg(jiraDimStyle.Render("  ·  loading…")))
	}
	segs = append(segs, plainSeg(jiraDimStyle.Render("  ·")))
	return append(segs, hintSegs(hint{label: "tab", press: "tab", what: "switch"}, keyHint(m.keys.Refresh, "refresh"),
		keyHint(m.keys.CopyKey, "copy"), hint{label: "esc", press: "esc", what: "board"})...)
}

// planSegs is planning's view line: the sprint's name steps to the next.
func (m *Model) planSegs() []headSeg {
	p := m.jiraTab.plan
	dim := jiraDimStyle.Render
	k := m.keys
	segs := []headSeg{plainSeg(jiraViewActive.Render("Planning") + dim("  backlog → ")), keySeg(dim(p.sprints[p.target].name), k.NextView)}
	if p.loading {
		segs = append(segs, plainSeg(dim("  ·  loading…")))
	}
	segs = append(segs, plainSeg(dim("  ·")))
	segs = append(segs, hintSegs(hint{"←", "left", "→", "right", "side"},
		hint{helpKey(k.PrevView), firstKey(k.PrevView), helpKey(k.NextView), firstKey(k.NextView), "sprint"})...)
	segs = append(segs, plainSeg(dim("  "+helpKey(k.MoveSprint)+"/space move across  K J rank  E goal  R rename  N new  S start/end  C C complete")))
	return append(segs, hintSegs(keyHint(k.OpenChannel, "open"), hint{label: "esc", press: "esc", what: "board"})...)
}
