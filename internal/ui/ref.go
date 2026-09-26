package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The issue side panel: the selected card, fetched and rendered on the right
// of the board. r refetches; o opens it in a browser; esc or v closes.
// Loading/rendering/keys live in jira.go; this file owns the panel machinery.

type refKind int

const (
	refJira refKind = iota
)

// reference is one openable issue.
type reference struct {
	kind    refKind
	jiraKey string // issue key, e.g. ABC-123
}

func (r reference) label() string { return r.jiraKey }

// Themed panel styles, set by applyTheme.
var refKeyStyle, refLabelStyle, refDimStyle, refErrStyle lipgloss.Style

// currentRef returns the reference currently shown, or nil when the panel is
// closed or the index is somehow out of range.
func (m *Model) currentRef() *reference {
	if !m.refOpen || m.refIdx < 0 || m.refIdx >= len(m.refs) {
		return nil
	}
	return &m.refs[m.refIdx]
}

// refStatusHint builds the status-bar line for the current ref.
func (m *Model) refStatusHint(r reference, n int) string {
	shared := helpKey(m.keys.OpenAttach) + " browser · " + helpKey(m.keys.Refresh) + " refresh · esc closes"
	edit := strings.Join([]string{
		helpKey(m.keys.JiraStatus), helpKey(m.keys.JiraPriority),
		helpKey(m.keys.JiraPoints), helpKey(m.keys.JiraAssignee),
	}, "/")
	return edit + " edit · " + helpKey(m.keys.JiraComment) + " comment · " +
		helpKey(m.keys.JiraReply) + " reply · " + helpKey(m.keys.JiraStart) + " start work · " + shared
}

// loadCurrentRef puts the panel into its loading state for the current ref and
// returns the fetch Cmd, bumping refGen so any older in-flight fetch is
// dropped on arrival.
func (m *Model) loadCurrentRef() tea.Cmd {
	r := m.currentRef()
	if r == nil {
		return nil
	}
	m.refGen++
	m.refLoading = true
	m.refErr = nil
	m.jiraIssue = nil
	m.refView.GotoTop()
	m.renderRef()
	return m.fetchJira(m.refGen, r.jiraKey)
}

// closeRef tears the panel down and returns focus to the board. refGen is
// bumped so any in-flight fetch is ignored on arrival.
func (m *Model) closeRef() {
	if !m.refOpen {
		return
	}
	m.refOpen = false
	m.refs = nil
	m.refBack = nil
	m.sizeRefView()
	m.refIdx = 0
	m.jiraIssue = nil
	m.refErr = nil
	m.refLoading = false
	m.refGen++
	// Tear down any open editor so it can't outlive the panel.
	m.closeJiraPicker()
	m.closeJiraField()
	m.closeJiraComment()
	m.clearPanelHint()
	if m.focus == focusRef {
		m.focus = focusJira
	}
	m.resize()
}

// refreshRef drops the cached copy of the shown issue and refetches it.
func (m Model) refreshRef() (tea.Model, tea.Cmd) {
	r := m.currentRef()
	if r == nil {
		return m, nil
	}
	m.jiraClient.Invalidate(r.jiraKey)
	cmd := m.loadCurrentRef()
	return m, cmd
}

// openCurrentRefURL opens the shown issue in a browser.
func (m Model) openCurrentRefURL() (tea.Model, tea.Cmd) {
	if m.currentRef() == nil || m.jiraIssue == nil || m.jiraIssue.URL == "" {
		return m, nil
	}
	o := openable{name: m.jiraIssue.Key, url: m.jiraIssue.URL}
	m.status = "opening " + o.url + "…"
	return m, m.openOpenable(o)
}

// handleRefKey owns every keystroke while the panel has focus: esc / the
// open-reference key close it, r refetches, o opens it in a browser, the
// Jira keys edit it, and anything else scrolls the viewport.
func (m Model) handleRefKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.panelFieldSel() != "" {
			m.clearPanelField()
			return m, nil
		}
		m.closeRef()
		return m, nil
	case "enter":
		if m.panelFieldSel() != "" {
			return m, m.editPanelField()
		}
	}
	switch {
	case key.Matches(msg, m.keys.Palette):
		m.openPalette()
		return m, nil
	case key.Matches(msg, m.keys.JiraDescription):
		return m, m.editDescription()
	case key.Matches(msg, m.keys.LogWork) && m.jiraIssue != nil:
		m.openWorklogInput(m.jiraIssue.Key, "", time.Time{})
		return m, nil
	case key.Matches(msg, m.keys.Timer):
		k := ""
		if m.jiraIssue != nil {
			k = m.jiraIssue.Key
		}
		return m, m.toggleTimer(k)
	case key.Matches(msg, m.keys.Timesheet):
		return m, m.openTimesheet()
	case key.Matches(msg, m.keys.Inbox):
		return m, m.openInbox()
	case key.Matches(msg, m.keys.Standup):
		return m, m.openStandup()
	case key.Matches(msg, m.keys.History):
		return m, m.openHistory()
	case key.Matches(msg, m.keys.NextView):
		return m, m.switchActivity(m.activityTab + 1)
	case key.Matches(msg, m.keys.PrevView):
		return m, m.switchActivity(m.activityTab - 1)
	case key.Matches(msg, m.keys.DevInfo):
		return m, m.openDevInfo()
	case key.Matches(msg, m.keys.Pin):
		if m.jiraIssue != nil {
			m.togglePin(m.jiraIssue.Key, m.jiraIssue.Summary)
		}
		return m, nil
	case key.Matches(msg, m.keys.IssueActions):
		m.openIssueActions()
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
		return m, nil
	case key.Matches(msg, m.keys.JiraLinks):
		m.openJiraLinkPicker()
		return m, nil
	case key.Matches(msg, m.keys.Image):
		m.openImageView()
		return m, nil
	case key.Matches(msg, m.keys.Back):
		if n := len(m.refBack); n > 0 {
			return m.backToCrumb(n - 1)
		}
		return m, nil
	case key.Matches(msg, m.keys.CopyKey), key.Matches(msg, m.keys.CopyURL):
		if m.jiraIssue != nil {
			return m, m.copyJira(m.jiraIssue.Key, key.Matches(msg, m.keys.CopyURL))
		}
	}
	switch {
	case key.Matches(msg, m.keys.OpenRef): // same key that opened it closes it
		m.closeRef()
		return m, nil
	case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.ShiftTab):
		if m.currentRef() == nil || m.jiraIssue == nil {
			m.focus = focusJira
			m.renderJira()
			return m, nil
		}
		d := 1
		if key.Matches(msg, m.keys.ShiftTab) {
			d = -1
		}
		m.movePanelField(d)
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		return m.refreshRef()
	case key.Matches(msg, m.keys.OpenAttach):
		return m.openCurrentRefURL()
	}
	// Jira keys, only once the issue is loaded; otherwise they fall through to
	// scroll the viewport.
	if m.currentRef() != nil && m.jiraIssue != nil {
		switch {
		case key.Matches(msg, m.keys.JiraStatus):
			return m, m.openJiraStatusPicker()
		case key.Matches(msg, m.keys.JiraPriority):
			return m, m.openJiraPriorityPicker()
		case key.Matches(msg, m.keys.JiraPoints):
			m.openJiraPointsInput()
			return m, nil
		case key.Matches(msg, m.keys.JiraSummary):
			m.openJiraSummaryInput()
			return m, nil
		case key.Matches(msg, m.keys.JiraLabels):
			m.openJiraLabelsInput()
			return m, nil
		case key.Matches(msg, m.keys.JiraAssignee):
			return m, m.openJiraAssigneePicker()
		case key.Matches(msg, m.keys.JiraComment):
			m.openJiraCommentInput()
			return m, nil
		case key.Matches(msg, m.keys.JiraReply):
			if len(m.jiraIssue.Comments) == 0 {
				m.status = "no comments to reply to"
				return m, nil
			}
			m.openJiraReplyPicker()
			return m, nil
		case key.Matches(msg, m.keys.JiraStart):
			return m, m.startJiraWork()
		}
	}
	var cmd tea.Cmd
	m.refView, cmd = m.refView.Update(msg)
	return m, cmd
}

// renderRef rebuilds the panel viewport's content (loading / error / the
// rendered issue).
func (m *Model) renderRef() {
	if !m.refOpen {
		return
	}
	r := m.currentRef()
	switch {
	case m.refErr != nil:
		m.refView.SetContent(refErrStyle.Render(m.refErr.Error()))
	case m.refLoading || r == nil:
		label := ""
		if r != nil {
			label = r.label()
		}
		m.refView.SetContent(refDimStyle.Render("loading " + label + "…"))
	case m.jiraIssue != nil:
		content := m.placeImages(expandTables(m.renderJiraIssue(m.jiraIssue, m.refView.Width()), m.refView.Width()))
		m.refView.SetContent(content)
		m.indexPanelHits(content)
	default:
		m.refView.SetContent(refDimStyle.Render("loading…"))
	}
}

// refMeta writes one aligned "label: value" row, skipping empty values. width
// is the column the values align to (label + colon, space-padded).
func refMeta(b *strings.Builder, label, value string, width int) {
	if value == "" {
		return
	}
	b.WriteString(refLabelStyle.Render(refMetaLabel(label, width)) + value + "\n")
}

// refField writes an editable field's row, "—" when empty, lit when the
// field cursor is on it.
func refField(b *strings.Builder, label, value string, width int, sel bool) {
	lbl := refMetaLabel(label, width)
	if sel {
		b.WriteString(selectedRow.Render(lbl+orDash(value)) + "\n")
		return
	}
	if value == "" {
		value = refDimStyle.Render("—")
	}
	b.WriteString(refLabelStyle.Render(lbl) + value + "\n")
}

func refMetaLabel(label string, width int) string {
	lbl := label + ":"
	return lbl + strings.Repeat(" ", max(width-len(lbl), 1))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// renderRefPane draws the bordered side pane: a title row + the scrollable
// detail viewport, with a scrollbar on the right border.
func (m *Model) renderRefPane(height, width int) string {
	innerH := max(height, 1)
	width = max(width, refPaneMinWidth)

	total := viewportVisualRows(m.refView.GetContent(), m.refView.Width())
	pct := scrollPercentFor(total, m.refView.Height(), m.refView.YOffset())
	showScrollbar := total > m.refView.Height() && pct < 1.0

	parts := append([]string{bar(titleStyle.Render(m.refPaneTitle()), width-3)}, m.crumbLines(width-3)...)
	content := lipgloss.JoinVertical(lipgloss.Left, append(parts, m.refView.View())...)

	borderColor := dimColor
	if m.focus == focusRef {
		borderColor = focusedColor
	}
	style := lipgloss.NewStyle().Border(border).UnsetBorderTop().UnsetBorderRight().
		Width(width - 1).Height(innerH).BorderForeground(borderColor)
	box := style.Render(content)

	rightBorder := renderRightBorder(innerH, 1, m.refView.Height(), total, pct, borderColor, showScrollbar, -1)
	return lipgloss.JoinHorizontal(lipgloss.Top, box, rightBorder)
}

// The panel's trail: the issues it came from by links, each a strip above
// the current one; backspace or a click on a strip goes back to it.

// refCrumb is an issue the panel showed before.
type refCrumb struct{ key, summary, status string }

// refCrumbsShown caps the strips; older ones fold into one line.
const refCrumbsShown = 3

// crumbLines are the trail's strips, width wide, oldest first.
func (m *Model) crumbLines(width int) []string {
	var out []string
	from := max(len(m.refBack)-refCrumbsShown, 0)
	if from > 0 {
		out = append(out, bar(refDimStyle.Render(fmt.Sprintf("↰ %d earlier", from)), width))
	}
	for _, c := range m.refBack[from:] {
		line := refKeyStyle.Render("↰ "+c.key) + "  " + refDimStyle.Render(c.status) + "  " + c.summary
		out = append(out, bar(ansi.Truncate(line, max(width, 1), "…"), width))
	}
	if len(out) > 0 && !shadeOn { // unshaded, a rule parts them from the issue
		out = append(out, refDimStyle.Render(strings.Repeat("─", max(width, 1))))
	}
	return out
}

// crumbRows is how many rows the trail takes above the panel's body.
func (m *Model) crumbRows() int {
	n := min(len(m.refBack), refCrumbsShown+1)
	if n > 0 && !shadeOn {
		n++
	}
	return n
}

// sizeRefView fits the panel's body under its title and trail.
func (m *Model) sizeRefView() {
	m.refView.SetHeight(max(m.bodyH()-2-m.crumbRows(), 1))
}

// backToCrumb shows trail entry i again, dropping it and what came after.
func (m Model) backToCrumb(i int) (tea.Model, tea.Cmd) {
	key := m.refBack[i].key
	m.refBack = m.refBack[:i]
	m.sizeRefView()
	m.selectJiraKey(key)
	m.renderJira()
	return m.showJiraKey(key)
}

// crumbAt is the trail entry on screen row y of the panel, -1 for none.
func (m *Model) crumbAt(y int) int {
	from := max(len(m.refBack)-refCrumbsShown, 0)
	row := y - 1 // the title's row first
	if from > 0 {
		if row == 0 {
			return from - 1 // "n earlier": the newest of them
		}
		row--
	}
	if row < 0 || from+row >= len(m.refBack) {
		return -1
	}
	return from + row
}

// refPaneTitle is the pane's heading.
func (m *Model) refPaneTitle() string {
	if m.refLoading && m.jiraIssue == nil {
		return "Jira (loading…)"
	}
	return "Jira"
}

// setPanelHint writes the panel's key hint into the status slot and
// remembers it, so it leaves with the panel — see clearPanelHint.
func (m *Model) setPanelHint(hint string) {
	m.status, m.panelHint = hint, hint
}

// clearPanelHint takes the panel's key hint back out of the status slot when
// the panel closes. Anything written there since is left alone.
func (m *Model) clearPanelHint() {
	if m.panelHint != "" && m.status == m.panelHint {
		m.status = ""
	}
	m.panelHint = ""
}
