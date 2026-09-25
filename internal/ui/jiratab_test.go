package ui

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// jiraTabModel sits on the Jira tab with a loaded scrum board: an active
// sprint with three lanes, then the backlog.
func jiraTabModel(t *testing.T) Model {
	t.Helper()
	m := configuredJiraModel(t, "ABC")
	m.width, m.height = 160, 40
	m.resize()
	out, _ := m.handleJiraBoard(jiraBoardMsg{
		seq: m.jiraTab.seq, project: "ABC",
		boards: []jira.Board{{ID: 1, Name: "ABC board", Type: "scrum"}},
		cfg: &jira.BoardConfig{Columns: []jira.Column{
			{Name: "To do", StatusIDs: []string{"1"}},
			{Name: "In progress", StatusIDs: []string{"3"}},
			{Name: "Done", StatusIDs: []string{"5", "6"}},
		}},
		views: []jiraView{
			{kind: jiraViewSprint, name: "Sprint 1", sprint: 9, lanes: true},
			{kind: jiraViewBacklog, name: "Backlog"},
		},
		quick: []jira.QuickFilter{{ID: 7, Name: "FE", JQL: "labels = frontend"}, {ID: 8, Name: "BE", JQL: "labels = backend"}},
		cards: []jira.Card{
			{Key: "ABC-1", Summary: "First", StatusID: "1", Status: "New", Assignee: "Ada", AssigneeID: "a1"},
			{Key: "ABC-2", Summary: "Second", StatusID: "3", Status: "In progress"},
			{Key: "ABC-3", Summary: "Third", StatusID: "1", Status: "New", Points: "5"},
			{Key: "ABC-4", Summary: "Fourth", StatusID: "6", Status: "Closed"},
		},
		total: 4,
	})
	return out.(Model)
}

func TestJiraTabLanes(t *testing.T) {
	m := jiraTabModel(t)
	if !m.jiraShowsLanes() {
		t.Fatal("active sprint should show as lanes")
	}
	var got []int
	for _, l := range m.jiraTab.lanes {
		got = append(got, len(l.cards))
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 1 || got[2] != 1 {
		t.Fatalf("lane sizes = %v, want [2 1 1]", got)
	}
	view := m.View().Content
	for _, want := range []string{"To do 2", "In progress 1", "ABC-3", "Sprint 1", "Backlog"} {
		if !strings.Contains(view, want) {
			t.Errorf("board lacks %q", want)
		}
	}
}

// TestJiraTabBacklogIsList: a planning view shows as a list whatever the mode.
func TestJiraTabBacklogIsList(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraCards(jiraCardsMsg{seq: m.jiraTab.seq, viewIdx: 1, cards: []jira.Card{
		{Key: "ABC-9", Summary: "Later", StatusID: "1", Status: "New"},
	}, total: 1})
	m = out.(Model)
	if m.jiraShowsLanes() {
		t.Fatal("backlog shows as lanes")
	}
	if m.toggleJiraMode(); !m.jiraTab.wantLanes {
		t.Error("toggle on a list-only view changed the mode")
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-9" {
		t.Errorf("selected %q, want ABC-9", c.Key)
	}
}

func TestJiraTabToggleKeepsSelection(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "right"))
	m = out.(Model)
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-2" {
		t.Fatalf("selected %q, want ABC-2", c.Key)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "t"))
	m = out.(Model)
	if m.jiraShowsLanes() {
		t.Fatal("t did not switch to the list")
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-2" {
		t.Errorf("list selected %q, want ABC-2", c.Key)
	}
}

// TestJiraTabMoveCard: L moves the card a lane right at once, the cursor
// follows it, and a transition is requested.
func TestJiraTabMoveCard(t *testing.T) {
	m := jiraTabModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "L"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("no transition requested")
	}
	if len(m.jiraTab.lanes[1].cards) != 2 {
		t.Fatalf("In progress has %d cards, want 2", len(m.jiraTab.lanes[1].cards))
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-1" || m.jiraTab.lane != 1 {
		t.Errorf("cursor on %q lane %d, want ABC-1 in lane 1", c.Key, m.jiraTab.lane)
	}
}

// TestJiraTabDragDrop: dragging a card onto another lane moves it there.
func TestJiraTabDragDrop(t *testing.T) {
	m := jiraTabModel(t)
	laneW := m.jiraTab.laneW
	y := jiraBodyTop + 1 // the first card's key line
	out, _ := m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.jiraTab.drag.active || m.jiraTab.drag.key != "ABC-1" {
		t.Fatalf("drag = %+v, want ABC-1 armed, not active", m.jiraTab.drag)
	}
	out, _ = m.Update(tea.MouseMotionMsg{X: 3, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.jiraTab.drag.active {
		t.Fatal("a one-column wiggle started the drag")
	}
	out, _ = m.Update(tea.MouseMotionMsg{X: laneW + 3, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.jiraTab.drag.over != 1 {
		t.Fatalf("over lane %d, want 1", m.jiraTab.drag.over)
	}
	// The ghost shows in In progress, above ABC-2 (ranked after ABC-1), and the
	// card it left stays faint in To do: ABC-1 is drawn twice.
	lanes := m.jiraTab.lanesOut
	if strings.Count(lanes, "┊ ABC-1") != 2 {
		t.Errorf("want the ghost and the left-behind card, got:\n%s", lanes)
	}
	if g, c := strings.Index(lanes, "┊ First"), strings.Index(lanes, "Second"); g < 0 || c < 0 || g > c {
		t.Errorf("ghost not above ABC-2:\n%s", lanes)
	}
	out, cmd := m.Update(tea.MouseReleaseMsg{X: laneW + 3, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil || m.jiraTab.drag.active {
		t.Fatal("drop did not move the card")
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-1" || m.jiraTab.lane != 1 {
		t.Errorf("cursor on %q lane %d, want ABC-1 in In progress", c.Key, m.jiraTab.lane)
	}
}

// TestJiraTabDropZones: a lane of several statuses splits into a zone per
// status while dragged over, and the drop lands on the zone's status.
func TestJiraTabDropZones(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.statusNames = map[string]string{"5": "Done", "6": "Closed"}
	laneW := m.jiraTab.laneW
	out, _ := m.Update(tea.MouseClickMsg{X: 2, Y: jiraBodyTop + 1, Button: tea.MouseLeft})
	m = out.(Model)
	zh := jiraZoneH(m.jiraTab.view.Height(), 2)
	y := jiraBodyTop + 1 + zh // inside the second zone
	out, _ = m.Update(tea.MouseMotionMsg{X: 2*laneW + 3, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.jiraTab.drag.over != 2 || m.jiraTab.drag.zone != 1 {
		t.Fatalf("drag = %+v, want lane 2 zone 1", m.jiraTab.drag)
	}
	for _, want := range []string{"Done", "Closed"} {
		if !strings.Contains(m.jiraTab.lanesOut, want) {
			t.Errorf("zones lack %q", want)
		}
	}
	out, cmd := m.Update(tea.MouseReleaseMsg{X: 2*laneW + 3, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("drop did not move the card")
	}
	if c, _ := m.selectedJiraCard(); c.StatusID != "6" || c.Status != "Closed" {
		t.Errorf("card = %+v, want it in Closed", c)
	}
}

// TestJiraTabKeyMoveAsksStatus: a keyboard move into a lane of several
// statuses asks which one.
func TestJiraTabKeyMoveAsksStatus(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.lane = 1
	m.clampJiraCursor()
	out, _ := m.handleJiraKey(keyMsg(t, "L"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickLaneStatus || len(m.jiraPicker.items) != 2 {
		t.Fatalf("picker = %+v, want the two Done statuses", m.jiraPicker)
	}
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if c, _ := m.selectedJiraCard(); cmd == nil || c.Key != "ABC-2" || c.StatusID != "6" {
		t.Errorf("card = %+v, want ABC-2 moving to status 6", c)
	}
}

func TestJiraTabOpensIssueInPanel(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.refOpen || m.focus != focusRef {
		t.Fatalf("refOpen=%v focus=%v, want the panel focused", m.refOpen, m.focus)
	}
	if r := m.refs[0]; r.kind != refJira || r.jiraKey != "ABC-1" {
		t.Fatalf("ref = %+v, want ABC-1", r)
	}
	if !strings.Contains(m.View().Content, "Jira") {
		t.Error("tab body missing")
	}
	m.closeRef()
	if m.focus != focusJira {
		t.Fatalf("after close focus=%v, want back on the board", m.focus)
	}
}

func TestJiraTabProjectPickerFilters(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.projects = []jira.Project{{Key: "XYZ", Name: "Other"}, {Key: "ABC", Name: "Alpha"}}
	out, _ := m.handleJiraKey(keyMsg(t, "p"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.items[0].id != "ABC" {
		t.Fatalf("picker = %+v, want configured ABC first", m.jiraPicker.items)
	}
	out, _ = m.handleJiraPickerKey(keyMsg(t, "x"))
	m = out.(Model)
	if len(m.jiraPicker.items) != 1 || m.jiraPicker.items[0].id != "XYZ" {
		t.Fatalf("filtered = %+v, want XYZ", m.jiraPicker.items)
	}
	out, cmd := m.handleJiraPickerKey(keyMsg(t, "enter"))
	m = out.(Model)
	if cmd == nil || m.jiraTab.project != "XYZ" || m.jiraPicker.active {
		t.Errorf("project=%q picker=%v, want XYZ loading", m.jiraTab.project, m.jiraPicker.active)
	}
}

func TestJiraFilterJQL(t *testing.T) {
	quick := []jira.QuickFilter{{ID: 7, JQL: "labels = a OR labels = b"}, {ID: 8, JQL: "x = 1"}}
	for _, c := range []struct {
		a    jiraAssignee
		on   map[int]bool
		want string
	}{
		{jiraAssignee{}, nil, ""},
		{jiraAssignee{id: "me"}, nil, "assignee = currentUser()"},
		{jiraAssignee{id: "none"}, map[int]bool{8: true}, "assignee is EMPTY AND (x = 1)"},
		{jiraAssignee{id: "a1"}, map[int]bool{7: true}, `assignee = "a1" AND (labels = a OR labels = b)`},
	} {
		if got := jiraFilterJQL(c.a, quick, c.on); got != c.want {
			t.Errorf("jiraFilterJQL(%+v, %v) = %q, want %q", c.a, c.on, got, c.want)
		}
	}
	if got := andJQL("a OR b", "c"); got != "(a OR b) AND (c)" {
		t.Errorf("andJQL = %q", got)
	}
}

// TestJiraTabQuickFilter: a digit toggles its quick filter and refetches.
func TestJiraTabQuickFilter(t *testing.T) {
	m := jiraTabModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "2"))
	m = out.(Model)
	if cmd == nil || !m.jiraTab.quickOn[8] || m.jiraTab.quickOn[7] {
		t.Fatalf("quickOn = %v, want BE on and a refetch", m.jiraTab.quickOn)
	}
	if !strings.Contains(m.View().Content, "2 BE") {
		t.Error("filter line lacks the quick filter")
	}
	out, _ = m.handleJiraKey(keyMsg(t, "0"))
	m = out.(Model)
	if m.jiraTab.jiraFiltered() {
		t.Error("0 left a filter on")
	}
}

// TestJiraTabAssigneeFilter: the picker offers the people on the board, and a
// pick filters by them.
func TestJiraTabAssigneeFilter(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "a"))
	m = out.(Model)
	items := m.jiraPicker.items
	if len(items) != 4 || items[3].id != "a1" || !items[0].current {
		t.Fatalf("items = %+v, want everyone, me, unassigned, Ada", items)
	}
	m.jiraPicker.idx = 3
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if cmd == nil || m.jiraTab.assignee.id != "a1" || m.jiraTab.assignee.label != "Ada" {
		t.Fatalf("assignee = %+v, want Ada and a refetch", m.jiraTab.assignee)
	}
}

// TestJiraTabSearch: / narrows the board locally, enter keeps the query, esc
// clears it.
func TestJiraTabSearch(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "/"))
	m = out.(Model)
	for _, k := range []string{"t", "h", "i"} {
		out, _ = m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	if len(m.jiraTab.order) != 1 || m.jiraTab.cards[m.jiraTab.order[0]].Key != "ABC-3" {
		t.Fatalf("order = %v, want only ABC-3", m.jiraTab.order)
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-3" {
		t.Errorf("selected %q, want ABC-3", c.Key)
	}
	out, _ = m.handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	if m.jiraTab.searching || m.jiraTab.jiraSearchQuery() != "thi" {
		t.Fatalf("after enter searching=%v query=%q", m.jiraTab.searching, m.jiraTab.jiraSearchQuery())
	}
	if !strings.Contains(m.View().Content, "/thi") {
		t.Error("board lacks the query chip")
	}
	out, _ = m.handleKey(keyMsg(t, "esc"))
	m = out.(Model)
	if m.jiraTab.jiraSearchQuery() != "" || len(m.jiraTab.order) != 4 {
		t.Errorf("after esc query=%q order=%v, want all cards", m.jiraTab.jiraSearchQuery(), m.jiraTab.order)
	}
}

func TestJiraTabSearchNoMatch(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "/"))
	m = out.(Model)
	out, _ = m.handleKey(keyMsg(t, "z"))
	m = out.(Model)
	if !strings.Contains(m.View().Content, "no issues match /z") {
		t.Error("empty search lacks its message")
	}
}

func TestHelpOverlay(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "?"))
	m = out.(Model)
	if !m.helpOpen || !strings.Contains(m.View().Content, "story points") {
		t.Fatal("? did not show help")
	}
	out, _ = m.handleKey(keyMsg(t, "j"))
	m = out.(Model)
	if m.helpOpen || m.jiraTab.row != 0 {
		t.Errorf("key after help: open=%v row=%d, want closed and swallowed", m.helpOpen, m.jiraTab.row)
	}
}

func TestJiraTabCopyKey(t *testing.T) {
	m := jiraTabModel(t)
	out, cmd := m.handleKey(keyMsg(t, "y"))
	m = out.(Model)
	if cmd == nil || m.status != "copied ABC-1" {
		t.Errorf("y: cmd=%v status=%q, want copied ABC-1", cmd != nil, m.status)
	}
	out, _ = m.handleKey(keyMsg(t, "Y"))
	m = out.(Model)
	if !strings.HasPrefix(m.status, "copied http") || !strings.HasSuffix(m.status, "/browse/ABC-1") {
		t.Errorf("Y: status=%q, want the browse URL", m.status)
	}
}

func TestJiraGotoKey(t *testing.T) {
	for in, want := range map[string]string{
		"abc-12": "ABC-12", " 42 ": "ABC-42", "XYZ-1": "XYZ-1", "abc": "", "-1": "", "": "",
	} {
		if got := jiraGotoKey(in, "ABC"); got != want {
			t.Errorf("jiraGotoKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJiraGotoOpensPanel(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "#"))
	m = out.(Model)
	if !m.jiraGotoActive {
		t.Fatal("# did not open the prompt")
	}
	out, _ = m.handleKey(keyMsg(t, "3"))
	m = out.(Model)
	out, cmd := m.handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	if m.jiraGotoActive || !m.refOpen || m.refs[0].jiraKey != "ABC-3" || cmd == nil {
		t.Fatalf("after enter: active=%v open=%v refs=%v", m.jiraGotoActive, m.refOpen, m.refs)
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-3" {
		t.Errorf("board cursor on %q, want ABC-3", c.Key)
	}
}

func TestJiraAutoRefresh(t *testing.T) {
	m := jiraTabModel(t)
	seq := m.jiraTab.seq
	out, cmd := m.handleJiraAutoRefresh()
	m = out.(Model)
	if cmd == nil || m.jiraTab.seq != seq {
		t.Fatalf("fresh board: seq %d→%d, want untouched and re-armed", seq, m.jiraTab.seq)
	}
	m.jiraTab.fetched = m.jiraTab.fetched.Add(-m.opts.staleAfter)
	m.helpOpen = true
	if out, _ = m.handleJiraAutoRefresh(); out.(Model).jiraTab.seq != seq {
		t.Error("refreshed under a modal")
	}
	m.helpOpen = false
	if out, _ = m.handleJiraAutoRefresh(); out.(Model).jiraTab.seq == seq {
		t.Error("stale idle board did not refresh")
	}
}

func TestJiraPriorityMark(t *testing.T) {
	for p, want := range map[string]string{"Highest": "⇈", "High": "↑", "Medium": "", "": "", "Low": "↓", "Lowest": "⇊"} {
		if got := ansi.Strip(jiraPriorityMark(p)); got != want {
			t.Errorf("jiraPriorityMark(%q) = %q, want %q", p, got, want)
		}
	}
	m := jiraTabModel(t)
	m.jiraTab.cards[0].Priority = "Highest"
	m.buildJiraLanes()
	m.renderJira()
	if !strings.Contains(m.View().Content, "⇈") {
		t.Error("lane card lacks its priority mark")
	}
}

func TestJiraListSort(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.wantLanes = false
	m.jiraTab.cards[1].Priority = "Highest"
	m.jiraTab.cards[3].Priority = "Low"
	m.buildJiraLanes()
	keys := func() string {
		var ks []string
		for _, ci := range m.jiraTab.order {
			ks = append(ks, m.jiraTab.cards[ci].Key)
		}
		return strings.Join(ks, " ")
	}
	rank := keys()
	out, _ := m.handleJiraKey(keyMsg(t, "s"))
	m = out.(Model)
	if got := keys(); m.jiraTab.sort != jiraSortPriority || got != "ABC-2 ABC-1 ABC-3 ABC-4" {
		t.Errorf("priority order = %q", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "s sort: priority") {
		t.Error("list lacks the sort chip")
	}
	out, _ = m.handleJiraKey(keyMsg(t, "s"))
	m = out.(Model)
	if got := keys(); !strings.HasPrefix(got, "ABC-3 ") {
		t.Errorf("points order = %q, want ABC-3 (5 pts) first", got)
	}
	for range jiraSortCount - 2 {
		out, _ = m.handleJiraKey(keyMsg(t, "s"))
		m = out.(Model)
	}
	if m.jiraTab.sort != jiraSortRank || keys() != rank {
		t.Errorf("full cycle: sort=%v order=%q, want rank %q", m.jiraTab.sort, keys(), rank)
	}
}

func TestJiraKeySortNumeric(t *testing.T) {
	cards := []jira.Card{{Key: "ABC-10"}, {Key: "ABC-9"}, {Key: "AB-100"}}
	order := []int{0, 1, 2}
	jiraSortKey.apply(order, cards)
	if order[0] != 2 || order[1] != 1 || order[2] != 0 {
		t.Errorf("order = %v, want AB-100 ABC-9 ABC-10", order)
	}
}

func TestJiraMineToggle(t *testing.T) {
	m := jiraTabModel(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "m"))
	m = out.(Model)
	if cmd == nil || m.jiraTab.assignee.id != "me" {
		t.Fatalf("m: assignee = %+v", m.jiraTab.assignee)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "m"))
	if a := out.(Model).jiraTab.assignee; a.id != "" {
		t.Errorf("second m: assignee = %+v, want everyone", a)
	}
}

func TestPanelBackHistory(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.openJiraKey("ABC-1")
	m = out.(Model)
	out, _ = m.openJiraKey("ABC-3")
	m = out.(Model)
	out, _ = m.openJiraKey("ABC-3") // reopening the same issue adds nothing
	m = out.(Model)
	if len(m.refBack) != 1 {
		t.Fatalf("history = %v, want [ABC-1]", m.refBack)
	}
	out, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = out.(Model)
	if cmd == nil || m.currentRef().jiraKey != "ABC-1" || len(m.refBack) != 0 {
		t.Fatalf("back: showing %v, history %v", m.currentRef(), m.refBack)
	}
	if c, _ := m.selectedJiraCard(); c.Key != "ABC-1" {
		t.Errorf("board cursor on %q, want ABC-1", c.Key)
	}
	m.closeRef()
	if m.refBack != nil {
		t.Error("closing kept the history")
	}
}

func TestPanelLinks(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	iss := &jira.Issue{Key: "ABC-1", Links: []jira.Link{{Rel: "blocks", Key: "ABC-3", Summary: "Third", Status: "New"}}}
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: iss})
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.refView.View()), "blocks ABC-3 Third · New") {
		t.Errorf("links section missing:\n%s", ansi.Strip(m.refView.View()))
	}
	out, _ = m.handleKey(keyMsg(t, "L"))
	m = out.(Model)
	if !m.jiraPicker.active || len(m.jiraPicker.items) != 1 {
		t.Fatalf("picker = %+v", m.jiraPicker)
	}
	out, cmd := m.handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	if cmd == nil || m.currentRef().jiraKey != "ABC-3" || len(m.refBack) != 1 {
		t.Errorf("after pick: showing %v, history %v", m.currentRef(), m.refBack)
	}
}

func TestJiraLaneWIPLimit(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.cfg.Columns[0].Max = 1
	m.buildJiraLanes()
	m.renderJira()
	if !strings.Contains(ansi.Strip(m.View().Content), "To do 2/1") {
		t.Fatal("lane head lacks its limit")
	}
	if !strings.Contains(m.jiraTab.lanesOut, jiraOverStyle.Underline(true).Render("To do 2/1")) {
		t.Error("over-limit lane not marked")
	}
}

func TestJiraCardShowsParent(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.cards[0].ParentKey, m.jiraTab.cards[0].ParentSummary = "ABC-100", "Checkout epic"
	m.buildJiraLanes()
	m.renderJira()
	if !strings.Contains(ansi.Strip(m.View().Content), "⌃ Checkout") {
		t.Error("card lacks its parent")
	}
	m.jiraTab.search.SetValue("checkout")
	m.applyJiraSearch()
	if len(m.jiraTab.order) != 1 {
		t.Errorf("search by parent matched %d cards, want 1", len(m.jiraTab.order))
	}
}

// TestRulesLogBoardChanges: the first fresh load is a baseline; a refresh
// logs what the rules match, a cached copy logs nothing.
func TestRulesLogBoardChanges(t *testing.T) {
	m := jiraTabModel(t)
	m.rulesLog = filepath.Join(t.TempDir(), "rules.log")
	m.rules, _ = rules.Compile([]rules.Rule{{Name: "moved", On: rules.StrList{"status"}, Actions: []rules.Action{{Type: "log"}}}})
	cards := append([]jira.Card(nil), m.jiraTab.cards...)
	if cmd := m.runRules(cards); cmd != nil {
		t.Fatal("baseline fired")
	}
	cards[1].StatusID, cards[1].Status = "5", "Done"
	cached := jiraCardsMsg{seq: m.jiraTab.seq, cached: true, cards: cards}
	if _, cmd := m.handleJiraCards(cached); cmd != nil {
		t.Error("cached cards fired")
	}
	_, cmd := m.handleJiraCards(jiraCardsMsg{seq: m.jiraTab.seq, cards: cards})
	if cmd == nil {
		t.Fatal("refresh did not fire")
	}
	if msg := cmd().(rulesLoggedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	b, _ := os.ReadFile(m.rulesLog)
	if !strings.HasSuffix(string(b), " moved: ABC-2 status In progress → Done\n") {
		t.Errorf("log = %q", b)
	}
}

// TestRuleNotifyAndExec: notify is an OSC 777 without the body's controls;
// exec gets templated args, env and the issue on stdin.
func TestRuleNotifyAndExec(t *testing.T) {
	if got := rules.NotifySeq("laneway", "ABC-2; done\x1b[31m"); got != "\x1b]777;notify;laneway;ABC-2, done[31m\x1b\\" {
		t.Errorf("notify = %q", got)
	}
	m := jiraTabModel(t)
	out := filepath.Join(t.TempDir(), "out")
	m.rules, _ = rules.Compile([]rules.Rule{{Actions: []rules.Action{{Type: "exec",
		Command: []string{"sh", "-c", `printf '%s %s ' "$1" "$LANEWAY_OLD_STATUS" > "$2"; cat >> "$2"`, "_", "{{.Key}}", out}}}}})
	cards := append([]jira.Card(nil), m.jiraTab.cards...)
	m.runRules(cards)
	cards[1].StatusID, cards[1].Status = "5", "Done"
	if msg := m.ruleExec(m.rules.Fire(rules.Diff(m.jiraTab.cards, cards)[0])[0])(); msg != nil {
		t.Fatal(msg)
	}
	b, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(b), `ABC-2 In progress {`) || !strings.Contains(string(b), `"Status":"Done"`) {
		t.Errorf("exec saw %q", b)
	}
	m.rules, _ = rules.Compile([]rules.Rule{{Name: "x", Actions: []rules.Action{{Type: "exec", Command: []string{"false"}}}}})
	if msg, _ := m.ruleExec(m.rules.Fire(rules.Event{Kind: "new", Card: cards[0]})[0])().(rulesLoggedMsg); msg.err == nil {
		t.Error("a failing command reported nothing")
	}
}

// TestRuleHighlight: a highlight rule marks the changed card in lanes and
// list until the card is opened.
func TestRuleHighlight(t *testing.T) {
	m := jiraTabModel(t)
	m.rules, _ = rules.Compile([]rules.Rule{{On: rules.StrList{"status"}, Actions: []rules.Action{{Type: "highlight"}}}})
	cards := append([]jira.Card(nil), m.jiraTab.cards...)
	m.runRules(cards)
	cards[1].StatusID, cards[1].Status = "5", "Done"
	out, _ := m.handleJiraCards(jiraCardsMsg{seq: m.jiraTab.seq, cards: cards})
	m = out.(Model)
	marked := regexp.MustCompile(`● ABC-2`)
	if !marked.MatchString(ansi.Strip(m.View().Content)) {
		t.Fatal("lanes lack the mark")
	}
	m.jiraTab.wantLanes = false
	m.renderJira()
	if !marked.MatchString(ansi.Strip(m.View().Content)) || strings.Contains(ansi.Strip(m.View().Content), "● ABC-1") {
		t.Fatal("list lacks the mark, or marks another card")
	}
	out, _ = m.showJiraKey("ABC-2")
	m = out.(Model)
	m.renderJira()
	if marked.MatchString(ansi.Strip(m.View().Content)) {
		t.Error("opening kept the mark")
	}
}

// TestRuleByMe: with by_me: false, a change you made fires nothing, one a
// teammate made does.
func TestRuleByMe(t *testing.T) {
	author := "me-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"me-1"}`))
		case "/rest/api/3/issue/ABC-2/changelog":
			fmt.Fprintf(w, `{"total":1,"values":[{"author":{"accountId":%q},"items":[{"fieldId":"status"}]}]}`, author)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	for _, c := range []struct {
		author string
		marked bool
	}{{"me-1", false}, {"bob", true}} {
		author = c.author
		m := jiraTabModel(t)
		m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
		no := false
		m.rules, _ = rules.Compile([]rules.Rule{{Match: rules.Match{ByMe: &no}, Actions: []rules.Action{{Type: "highlight"}}}})
		cards := append([]jira.Card(nil), m.jiraTab.cards...)
		m.runRules(cards)
		cards[1].StatusID, cards[1].Status = "5", "Done"
		msg := m.runRules(cards)().(rulesEventsMsg)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
		out, _ := m.handleRulesEvents(msg)
		if _, got := out.(Model).jiraTab.highlights["ABC-2"]; got != c.marked {
			t.Errorf("author %s: marked = %v, want %v", c.author, got, c.marked)
		}
	}
}

// TestRuleWatch: a watch's first search is a baseline, the next fires its
// rules and not the board's, and every poll arms the next.
func TestRuleWatch(t *testing.T) {
	m := jiraTabModel(t)
	m.rules, _ = rules.Compile([]rules.Rule{
		{Name: "board", Actions: []rules.Action{{Type: "highlight", Color: "1"}}},
		{Name: "mine", Watch: "assignee = currentUser()", Actions: []rules.Action{{Type: "highlight", Color: "2"}}},
	})
	cards := []jira.Card{{Key: "XYZ-1", StatusID: "1", Status: "To do"}}
	out, cmd := m.handleRuleWatched(ruleWatchedMsg{jql: "assignee = currentUser()", cards: cards})
	m = out.(Model)
	if cmd == nil || len(m.jiraTab.highlights) != 0 {
		t.Fatalf("baseline: cmd %v, highlights %v", cmd, m.jiraTab.highlights)
	}
	cards = []jira.Card{{Key: "XYZ-1", StatusID: "2", Status: "Done"}}
	out, _ = m.handleRuleWatched(ruleWatchedMsg{jql: "assignee = currentUser()", cards: cards})
	m = out.(Model)
	if got := m.jiraTab.highlights; len(got) != 1 || got["XYZ-1"] != "2" {
		t.Errorf("highlights = %v", got)
	}
	out, cmd = m.handleRuleWatched(ruleWatchedMsg{jql: "assignee = currentUser()", err: errors.New("down")})
	if m = out.(Model); cmd == nil || !strings.Contains(m.status, "down") {
		t.Errorf("failed poll: cmd %v, status %q", cmd, m.status)
	}
}

// TestJiraMoveToSprint: M offers the sprints and the backlog, the current
// one marked; the current is a no-op, the backlog posts the card there.
func TestJiraMoveToSprint(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = r.Method + " " + r.URL.Path + " " + string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	c, _ := m.selectedJiraCard()
	out, _ := m.handleJiraKey(keyMsg(t, "M"))
	m = out.(Model)
	p := m.jiraPicker
	if !p.active || p.kind != jiraPickSprint || p.issueKey != c.Key || len(p.items) != 2 ||
		!p.items[0].current || p.items[1].id != jiraBacklogID {
		t.Fatalf("picker = %+v", p)
	}
	if !strings.Contains(m.View().Content, "Move "+c.Key+" to") {
		t.Error("picker not drawn")
	}
	if _, cmd := m.applyJiraPick(); cmd != nil {
		t.Error("the current sprint should be a no-op")
	}
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	if cmd == nil || out.(Model).jiraPicker.active {
		t.Fatal("expected a move")
	}
	if msg := cmd().(jiraMutatedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if want := `POST /rest/agile/1.0/backlog/issue {"issues":["` + c.Key + `"]}`; got != want {
		t.Errorf("request = %q, want %q", got, want)
	}
}

// TestJiraSprintLine: a sprint view shows days left or when it starts or
// ended, then its goal on one line; other views show nothing.
func TestJiraSprintLine(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	day := 24 * time.Hour
	for _, tc := range []struct {
		v    jiraView
		want string
	}{
		{jiraView{kind: jiraViewSprint, start: now.Add(-day), end: now.Add(4*day + time.Hour), goal: "Ship\nit"}, "5d left · Ship it"},
		{jiraView{kind: jiraViewSprint, start: now.Add(3 * day)}, "starts Sep 28"},
		{jiraView{kind: jiraViewSprint, end: now.Add(-day)}, "ended Sep 24"},
		{jiraView{kind: jiraViewSprint, goal: "Goal only"}, "Goal only"},
		{jiraView{kind: jiraViewBacklog, goal: "x"}, ""},
	} {
		if got := jiraSprintLine(tc.v, now); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.v, got, tc.want)
		}
	}
	m := jiraTabModel(t)
	m.jiraTab.views[0].end = time.Now().Add(2*day + time.Hour)
	m.jiraTab.views[0].goal = "Launch"
	if !strings.Contains(m.View().Content, "3d left · Launch") {
		t.Error("header lacks the sprint line")
	}
}

// TestJiraLanePoints: a lane head sums its estimated cards, and shows no
// sum when none is estimated or points are hidden.
func TestJiraLanePoints(t *testing.T) {
	cards := []jira.Card{{Points: "3"}, {Points: ""}, {Points: "0.1"}, {Points: "0.2"}}
	if got, ok := jiraLanePoints(cards, []int{0, 1, 2, 3}); !ok || got != "3.3" {
		t.Errorf("sum = %q %v", got, ok)
	}
	if _, ok := jiraLanePoints(cards, []int{1}); ok {
		t.Error("an unestimated lane has no sum")
	}
	m := jiraTabModel(t)
	if !strings.Contains(m.View().Content, "· 5p") {
		t.Error("lane head lacks its points")
	}
	m.opts.fields.points = false
	m.renderJira()
	if strings.Contains(m.View().Content, "· 5p") {
		t.Error("hidden points still summed")
	}
}

// TestPRMark: a card with an open pull request says so; card_fields can
// leave it out.
func TestPRMark(t *testing.T) {
	c := jira.Card{Key: "ABC-9", Summary: "S", PR: "OPEN"}
	lines := jiraCardLines(c, true, allCardFields)
	if !strings.Contains(ansi.Strip(lines[0]), "ABC-9") || !strings.Contains(ansi.Strip(lines[0]), "PR") {
		t.Errorf("head = %q", ansi.Strip(lines[0]))
	}
	c.PR = "MERGED"
	if got := ansi.Strip(jiraCardLines(c, true, allCardFields)[0]); !strings.Contains(got, "✓PR") {
		t.Errorf("merged head = %q", got)
	}
	f := allCardFields
	f.pr = false
	if got := ansi.Strip(jiraCardLines(c, true, f)[0]); strings.Contains(got, "PR") {
		t.Errorf("pr off: %q", got)
	}
}
