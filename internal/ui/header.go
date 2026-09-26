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
	s    string // as drawn
	kind string // a click: "view", "quick", "term", "assignee", "esc", "key", "" nothing
	i    int    // the view, quick filter or term
	bind key.Binding
}

func plainSeg(s string) headSeg { return headSeg{s: s} }

func keySeg(s string, b key.Binding) headSeg { return headSeg{s: s, kind: "key", bind: b} }

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
	if t.roadmap != nil || t.plan != nil || t.charts != nil {
		return headSeg{}
	}
	var segs []headSeg
	switch y {
	case 0:
		segs = m.jiraTitleSegs()
	case jiraBodyTop - 2:
		segs = m.jiraViewSegs()
	case jiraBodyTop - 1:
		if t.searching {
			return headSeg{}
		}
		segs = m.jiraFilterSegs()
	}
	at := 1 // the box's left border
	for _, s := range segs {
		w := ansi.StringWidth(s.s)
		if x >= at && x < at+w {
			return s
		}
		at += w
	}
	return headSeg{}
}

// clickHeader acts on a header hit; ok false when x, y is none.
func (m Model) clickHeader(x, y int) (tea.Model, tea.Cmd, bool) {
	h := m.headerHit(x, y)
	switch h.kind {
	case "view":
		return m, m.cycleJiraView(h.i - m.jiraTab.viewIdx), true
	case "quick":
		return m, m.toggleJiraQuick(h.i), true
	case "term":
		m.removeSearchTerm(h.i)
		return m, nil, true
	case "esc":
		m.clearJiraSearch()
		return m, nil, true
	case "assignee":
		h.bind = m.keys.Assignee
	case "key":
	default:
		return m, nil, false
	}
	if len(h.bind.Keys()) == 0 {
		return m, nil, true
	}
	out, cmd := m.handleJiraKey(keyPress(h.bind.Keys()[0]))
	return out, cmd, true
}
