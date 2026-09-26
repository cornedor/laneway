package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Clicks in the issue panel: a field is selected by one click and edited by
// a second (or a double-click); a linked issue opens.

// panelHit is what a panel line does when clicked: url opens in the
// browser, field >= 0 selects that field, else key opens that issue.
type panelHit struct {
	field int
	key   string
	url   string // a link's target (panelLinkAt)
}

// indexPanelHits finds the clickable lines of the panel's content: the
// fields where the render wrote them, the links under their heading.
func (m *Model) indexPanelHits(content string) {
	m.panelHits = map[int]panelHit{}
	for i, l := range m.panelFieldLine {
		m.panelHits[l] = panelHit{field: i}
	}
	iss := m.jiraIssue
	m.activityLine = -1
	if iss != nil {
		labels := activityLabels(max(iss.CommentTotal, len(iss.Comments)))
		tabs := strings.Join(labels[:], "  ") + "   [ ]"
		for i, l := range strings.Split(content, "\n") {
			if strings.TrimSpace(ansi.Strip(l)) == tabs {
				m.activityLine = i
			}
		}
	}
	if iss == nil || len(iss.Links) == 0 {
		return
	}
	head := fmt.Sprintf("Links (%d)  L open", len(iss.Links))
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if strings.TrimSpace(ansi.Strip(l)) != head {
			continue
		}
		for j, lk := range iss.Links {
			if i+1+j < len(lines) {
				m.panelHits[i+1+j] = panelHit{field: -1, key: lk.Key}
			}
		}
		return
	}
}

// panelLineAt is the content line on screen row y of the panel, -1 above
// its body. The body soft-wraps, so rows count wrapped lines.
func (m *Model) panelLineAt(y int) int {
	i, _ := m.panelCellAt(y)
	return i
}

// panelCellAt is the content line on screen row y and which of its wrapped
// rows that is.
func (m *Model) panelCellAt(y int) (line, wrap int) {
	top := 1 + m.crumbRows() // the title, then the trail
	row := y - top
	if row < 0 || row >= m.refView.Height() {
		return -1, 0
	}
	v := row + m.refView.YOffset()
	lines := strings.Split(m.refView.GetContent(), "\n")
	w := m.refView.Width()
	seen := 0
	for i, l := range lines {
		n := visualRowsBefore([]string{l}, 1, w)
		if v < seen+n {
			return i, v - seen
		}
		seen += n
	}
	return -1, 0
}

// panelLinkAt is the URL of the OSC 8 link under screen cell x, y of the
// panel, "" for none. Mouse capture keeps the terminal from opening links
// itself, so the panel does.
func (m *Model) panelLinkAt(x, y int) string {
	i, wrap := m.panelCellAt(y)
	if i < 0 {
		return ""
	}
	listW, _ := m.jiraListWidth(m.width)
	col := x - listW - 1 + wrap*m.refView.Width() // after the pane's left border
	return linkAt(strings.Split(m.refView.GetContent(), "\n")[i], col)
}

// linkAt is the OSC 8 link target at display column col of line, "" when
// col falls on plain text.
func linkAt(line string, col int) string {
	url, at := "", 0
	for len(line) > 0 {
		if strings.HasPrefix(line, "\x1b]") { // OSC: a link opens or closes
			end, n := len(line), 0
			if j := strings.Index(line, "\x1b\\"); j >= 0 {
				end, n = j, 2
			}
			if j := strings.IndexByte(line, '\a'); j >= 0 && j < end {
				end, n = j, 1
			}
			if body := line[2:end]; strings.HasPrefix(body, "8;") {
				_, url, _ = strings.Cut(body[2:], ";")
			}
			line = line[min(end+n, len(line)):]
			continue
		}
		seq, w, n, _ := ansi.DecodeSequence(line, ansi.NormalState, nil)
		if w > 0 {
			if col >= at && col < at+w {
				return url
			}
			at += w
		}
		if n <= 0 {
			n = len(seq)
		}
		line = line[max(n, 1):]
	}
	return ""
}

// clickPanel acts on a clicked panel line.
func (m Model) clickPanel(h panelHit, count int) (tea.Model, tea.Cmd) {
	if h.url != "" {
		m.status = "opening " + h.url + "…"
		return m, m.openOpenable(openable{name: h.url, url: h.url})
	}
	if h.field < 0 {
		return m.openJiraKey(h.key)
	}
	if m.panelFieldIdx() == h.field || count == 2 {
		m.fieldCursor, m.fieldCursorKey = h.field, m.jiraIssue.Key
		return m, m.editPanelField()
	}
	m.fieldCursor, m.fieldCursorKey = h.field, m.jiraIssue.Key
	m.renderRef()
	return m, nil
}
