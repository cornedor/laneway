package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// viewLineOf is the screen row of the first line containing s, -1 for none.
func viewLineOf(m Model, s string) int {
	for i, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func click(m Model, x, y int) Model {
	out, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return out.(Model)
}

// TestClickSwimlaneHeader: a click on a band's header folds it, again
// unfolds it.
func TestClickSwimlaneHeader(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "s"))
	m = out.(Model)
	y := viewLineOf(m, "▾ AD Ada")
	if y < 0 {
		t.Fatalf("no Ada band:\n%s", ansi.Strip(m.View().Content))
	}
	if m = click(m, 5, y); viewLineOf(m, "▸ AD Ada · 1") < 0 || viewLineOf(m, "First") >= 0 {
		t.Fatalf("click did not fold:\n%s", ansi.Strip(m.View().Content))
	}
	if c, _ := m.selectedJiraCard(); c.Key == "ABC-1" {
		t.Error("the cursor stayed in the fold")
	}
	if m = click(m, 5, viewLineOf(m, "▸ AD Ada")); viewLineOf(m, "First") < 0 {
		t.Error("click again should unfold")
	}
}

// TestClickAboveBoard: a click on the header's blank space leaves the
// cursor's lane alone.
func TestClickAboveBoard(t *testing.T) {
	m := jiraTabModel(t)
	lane := m.jiraTab.lane
	if m = click(m, m.jiraTab.laneW+3, 1); m.jiraTab.lane != lane {
		t.Errorf("lane %d, want %d", m.jiraTab.lane, lane)
	}
}

// TestClickClosesHelp: any click closes the help overlay.
func TestClickClosesHelp(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "?"))
	if m = click(out.(Model), 3, 3); m.helpOpen {
		t.Error("help still open")
	}
}

// TestClickChartTab: a click on a chart's name in the view line shows it.
func TestClickChartTab(t *testing.T) {
	m := chartsModel(t)
	line := strings.Split(ansi.Strip(m.View().Content), "\n")[jiraBodyTop-2]
	x := ansi.StringWidth(line[:strings.Index(line, "Flow")])
	if m = click(m, x+1, jiraBodyTop-2); m.jiraTab.charts.tab != 2 {
		t.Errorf("tab %d, want flow", m.jiraTab.charts.tab)
	}
	if m = click(m, 0, jiraBodyTop-2); m.jiraTab.charts.tab != 2 {
		t.Error("a click on the border switched")
	}
}

// clickText clicks the first screen cell showing s, failing when none does.
func clickText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(l, s); i >= 0 {
			return click(m, ansi.StringWidth(l[:i]), y)
		}
	}
	t.Fatalf("no %q on screen:\n%s", s, ansi.Strip(m.View().Content))
	return m
}

// TestClickSettings: a click selects a row, another on it edits, outside
// cancels the edit, then closes.
func TestClickSettings(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, ","))
	m = out.(Model)
	i := 3
	name := m.settings.rows[i].name
	for j, r := range m.settings.rows {
		if j != i && strings.Contains(r.name, name) {
			t.Fatalf("%s is not a unique name", name)
		}
	}
	if m = clickText(t, m, name); m.settings.idx != i || m.settings.input != nil {
		t.Fatalf("idx %d, want %d, not editing", m.settings.idx, i)
	}
	if m = clickText(t, m, name); m.settings.input == nil {
		t.Fatal("a click on the selected row should edit it")
	}
	if m = click(m, 0, 0); m.settings == nil || m.settings.input != nil {
		t.Fatal("outside should cancel the edit only")
	}
	if m = click(m, 0, 0); m.settings != nil {
		t.Error("outside should close")
	}
}

// TestClickFilterBuilder: a field's click moves on to compare, a value's
// adds the term.
func TestClickFilterBuilder(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "F"))
	m = out.(Model)
	if m = clickText(t, m, "Assignee"); m.builderPick(0).id != "assignee" || m.filterBuilder.col != 1 {
		t.Fatalf("field %q col %d", m.builderPick(0).id, m.filterBuilder.col)
	}
	if m = clickText(t, m, "Ada · 1"); m.jiraTab.search.Value() != "assignee:Ada" {
		t.Errorf("query %q", m.jiraTab.search.Value())
	}
	if m = click(m, 0, 0); m.filterBuilder != nil {
		t.Error("outside should close")
	}
}

// TestClickJQL: a click on a completion takes it; outside cancels.
func TestClickJQL(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "Q"))
	m = out.(Model)
	m.jql.input.SetValue("")
	m.jql.sugg = []string{"assignee = currentUser()", "status = Done"}
	if m = clickText(t, m, "status = Done"); m.jql.input.Value() != "status = Done" {
		t.Errorf("input %q", m.jql.input.Value())
	}
	if m = click(m, 0, 0); m.jql != nil {
		t.Error("outside should cancel")
	}
}

// TestWheelOverlay: the wheel moves the settings cursor, not the board.
func TestWheelOverlay(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, ","))
	out, _ = out.(Model).Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if m = out.(Model); m.settings.idx != 1 || m.jiraTab.row != 0 {
		t.Errorf("idx %d, board row %d", m.settings.idx, m.jiraTab.row)
	}
}

// TestClickImageView: the right half steps on, the left back, the wheel
// too; a click below the image leaves the viewer.
func TestClickImageView(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraIssue = &jira.Issue{Key: "ABC-1", Attachments: []jira.Attachment{{ID: "10"}, {ID: "11"}}}
	m.images = &panelImages{on: true, maxRows: 16, byAtt: map[string]*panelImage{
		"10": {state: imgReady, id: 1, pxW: 100, pxH: 100},
		"11": {state: imgReady, id: 2, pxW: 100, pxH: 100},
	}}
	m.openImageView()
	mid := m.bodyH() / 2
	if m = click(m, m.width-5, mid); m.imageViewIdx != 1 {
		t.Fatalf("right half: image %d", m.imageViewIdx)
	}
	if m = click(m, 5, mid+1); m.imageViewIdx != 0 {
		t.Fatalf("left half: image %d", m.imageViewIdx)
	}
	out, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: mid, Button: tea.MouseWheelDown})
	if m = out.(Model); m.imageViewIdx != 1 {
		t.Fatalf("wheel: image %d", m.imageViewIdx)
	}
	if m = click(m, 5, m.bodyH()-1); m.imageView {
		t.Error("a click below the image should go back")
	}
}

// TestClickOutsideInputs: outside the go-to and create boxes a click
// cancels them; inside it keeps them.
func TestClickOutsideInputs(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "#"))
	m = out.(Model)
	if !m.jiraGotoActive {
		t.Fatal("# did not open go-to")
	}
	if m = clickText(t, m, "Go to issue"); !m.jiraGotoActive {
		t.Fatal("a click inside closed it")
	}
	if m = click(m, 0, 0); m.jiraGotoActive {
		t.Error("outside should cancel go-to")
	}
	m.openJiraCreateSummary("Task")
	if m = click(m, 0, 0); m.jiraCreateActive {
		t.Error("outside should cancel create")
	}
}

// TestClickHeaderKeys: the header's key hints, names and chips run their
// key.
func TestClickHeaderKeys(t *testing.T) {
	m := jiraTabModel(t)
	if m = clickText(t, m, "? help"); !m.helpOpen {
		t.Fatal("help chip")
	}
	m.helpOpen = false
	lanes := m.jiraShowsLanes()
	if m = clickText(t, m, "lanes/list"); m.jiraShowsLanes() == lanes {
		t.Error("lanes/list chip did not switch")
	}
	if m = clickText(t, m, "/ search"); !m.jiraTab.searching {
		t.Fatal("search chip")
	}
	out, _ := m.handleKey(keyMsg(t, "f"))
	out, _ = out.(Model).handleKey(keyMsg(t, "enter"))
	if m = clickText(t, out.(Model), "esc"); m.jiraTab.jiraSearchQuery() != "" {
		t.Error("esc chip left the query")
	}
	m.jiraTab.offline = "down"
	m.renderJira()
	if m = clickText(t, m, "offline"); !m.jiraTab.loading {
		t.Error("offline notice did not retry")
	}
}

// TestClickHeaderTimer: the running timer's label stops it into the log
// input.
func TestClickHeaderTimer(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleKey(keyMsg(t, "T"))
	if m = out.(Model); m.timerLabel() == "" {
		t.Fatal("T started no timer")
	}
	if m = clickText(t, m, "⏱"); !m.worklogFromTimer {
		t.Error("a click on the timer should open its log")
	}
}

// TestClickRoadmapFold: a click on an epic's ▸ folds its children out,
// again in; a click on a view line hint presses its key.
func TestClickRoadmapFold(t *testing.T) {
	m := roadmapModel(t)
	y := viewLineOf(m, "▸ ABC-10")
	if y < 0 {
		t.Fatalf("no fold mark:\n%s", ansi.Strip(m.View().Content))
	}
	if m = click(m, 1, y); viewLineOf(m, "ABC-12 Pay") < 0 {
		t.Fatal("click did not fold out")
	}
	if m = click(m, 1, y); viewLineOf(m, "ABC-12 Pay") >= 0 {
		t.Error("click again should fold in")
	}
	zoom := m.jiraTab.roadmap.zoom
	if m = clickText(t, m, "+ - zoom"); m.jiraTab.roadmap.zoom == zoom {
		t.Error("+ hint did not zoom")
	}
	if m = clickText(t, m, "space children"); viewLineOf(m, "ABC-12 Pay") < 0 {
		t.Error("space hint did not fold out")
	}
}

// TestClickPlanSprint: planning's sprint name steps to the next sprint.
func TestClickPlanSprint(t *testing.T) {
	m := planModel(t, nil)
	p := m.jiraTab.plan
	p.sprints = append(p.sprints, p.sprints[0])
	p.sprints[len(p.sprints)-1].name = "Sprint 9"
	m.renderJira()
	target := p.target
	if m = clickText(t, m, p.sprints[target].name); m.jiraTab.plan.target == target {
		t.Error("the name did not step")
	}
}

// TestPickerWrappedTitle: a title wrapped over lines on a narrow terminal
// still maps a click to the row under it.
func TestPickerWrappedTitle(t *testing.T) {
	m := jiraTabModel(t)
	m.openPalette()
	m.jiraPicker.title = strings.Repeat("a long title ", 8)
	m.width = 60
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	want := m.jiraPicker.items[2].label
	y := -1
	for i, l := range lines {
		if strings.Contains(l, want) {
			y = i
			break
		}
	}
	if i, _ := m.pickerRowAt(30, y); i != 2 {
		t.Errorf("row at y %d = %d, want 2", y, i)
	}
}

// TestClickPlanHead: a click on a side's head focuses that side.
func TestClickPlanHead(t *testing.T) {
	m := planModel(t, nil)
	m.jiraTab.plan.side = 0
	if m = click(m, m.width-10, jiraBodyTop); m.jiraTab.plan.side != 1 {
		t.Error("the sprint's head did not take the focus")
	}
}

// panelModel is the board with ABC-1 loaded in the panel: a link, an image
// ready to draw.
func panelModel(t *testing.T) Model {
	t.Helper()
	m := jiraTabModel(t)
	m.images = &panelImages{on: true, maxRows: 16, byAtt: map[string]*panelImage{}}
	iss := &jira.Issue{Key: "ABC-1", Summary: "s", Description: "Words\n\n![shot.png](attachment:10)",
		Attachments: []jira.Attachment{{ID: "10", Filename: "shot.png", MimeType: "image/png"}},
		Links:       []jira.Link{{Rel: "blocks", Key: "ABC-2", Summary: "Second"}}}
	out, _ := openRefFor(m, "ABC-1")
	out, _ = out.(Model).handleJiraLoaded(jiraLoadedMsg{gen: out.(Model).refGen, key: "ABC-1", issue: iss})
	m = out.(Model)
	e := m.images.byAtt["10"]
	out, _ = m.handleImageLoaded(imageLoadedMsg{att: "10", id: e.id, pxW: 40, pxH: 40, cols: 4, rows: 2, seq: "SEQ"})
	return out.(Model)
}

// TestClickPanelExtras: the hint line's keys, the Links head, a double-click
// on the Description head and an inline image each do theirs.
func TestClickPanelExtras(t *testing.T) {
	m := panelModel(t)
	if m = clickText(t, m, "c comment"); !m.jiraCommentActive {
		t.Fatal("hint did not open the composer")
	}
	m.jiraCommentActive = false
	if m = clickText(t, m, "Links (1)"); !m.jiraPicker.active {
		t.Fatal("Links head did not open the link picker")
	}
	m.jiraPicker.active = false
	m.renderRef()
	if m = clickText(t, m, "Description"); strings.Contains(m.status, "description") {
		t.Fatal("one click should not edit")
	}
	if m = clickText(t, m, "Description"); !strings.Contains(m.status, "loading ABC-1 description") {
		t.Fatalf("a double-click should edit the description: %q", m.status)
	}
	if m = clickText(t, m, "\U0010EEEE"); !m.imageView {
		t.Error("a click on the image should show it full size")
	}
}

// TestWheelPanelWhileEditing: with a comment composed in the panel the
// wheel scrolls the panel and leaves the board alone.
func TestWheelPanelWhileEditing(t *testing.T) {
	m := panelModel(t)
	m.jiraIssue.Description = strings.Repeat("line\n\n", 60)
	m.renderRef()
	m.focus = focusRef
	out, _ := m.Update(keyMsg(t, "c"))
	if m = out.(Model); !m.jiraCommentActive || !m.commentInline() {
		t.Fatal("c did not compose in the panel")
	}
	listW, _ := m.jiraListWidth(m.width)
	top := m.refView.YOffset()
	out, _ = m.Update(tea.MouseWheelMsg{X: listW + 5, Y: 5, Button: tea.MouseWheelUp})
	if m = out.(Model); top == 0 || m.refView.YOffset() != top-3 {
		t.Errorf("the panel did not scroll: %d → %d", top, m.refView.YOffset())
	}
	row := m.jiraTab.row
	out, _ = m.Update(tea.MouseWheelMsg{X: 3, Y: 6, Button: tea.MouseWheelDown})
	if m = out.(Model); m.jiraTab.row != row || !m.jiraCommentActive {
		t.Error("the board moved under the edit")
	}
}
