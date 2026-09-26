package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Clicks in the issue panel: a field is selected by one click and edited by
// a second (or a double-click); a linked issue opens.

// panelHit is what a panel line does when clicked: url opens in the
// browser, reply answers comment field, image shows that attachment full
// size, press presses a key (on a double-click when double), hints presses
// the hint under col, field >= 0 selects that field, else key opens that
// issue.
type panelHit struct {
	field  int
	key    string
	url    string // a link's target (panelLinkAt)
	reply  bool
	image  string
	press  string
	double bool
	hints  bool
	col    int
}

// panelHintLine is the panel's line of edit keys; a click on one presses it.
const panelHintLine = "tab fields · ↵ edit · c comment · R reply · S start work · ? keys"

// imageFG finds an image placeholder's id, written as its foreground.
var imageFG = regexp.MustCompile("\x1b\\[38;2;(\\d+);(\\d+);(\\d+)m\U0010EEEE")

// indexPanelHits finds the clickable lines of the panel's content: the
// fields where the render wrote them, the links under their heading.
func (m *Model) indexPanelHits(content string) {
	m.panelHits = map[int]panelHit{}
	for i, l := range m.panelFieldLine {
		m.panelHits[l] = panelHit{field: i}
	}
	iss := m.jiraIssue
	m.activityLine = -1
	if iss == nil {
		return
	}
	labels := activityLabels(max(iss.CommentTotal, len(iss.Comments)))
	tabs := strings.Join(labels[:], "  ") + "   [ ]"
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		switch text := strings.TrimSpace(ansi.Strip(l)); {
		case text == tabs:
			m.activityLine = i
		case text == panelHintLine:
			m.panelHits[i] = panelHit{field: -1, hints: true}
		case text == "Description" && m.descEdit == nil:
			m.panelHits[i] = panelHit{field: -1, press: "E", double: true}
		case strings.HasPrefix(text, "…and ") && strings.HasSuffix(text, "o opens in browser"):
			m.panelHits[i] = panelHit{field: -1, press: "o"}
		}
		if att := m.imageOn(l); att != "" {
			m.panelHits[i] = panelHit{field: -1, image: att}
		}
	}
	// The bylines in drawing order, each found after the one before.
	if at := m.activityLine; at >= 0 {
		heads := m.commentHeads
		for i := at + 1; i < len(lines) && len(heads) > 0; i++ {
			if strings.TrimSpace(ansi.Strip(lines[i])) == heads[0].text {
				m.panelHits[i] = panelHit{field: heads[0].i, reply: true}
				heads = heads[1:]
			}
		}
	}
	if len(iss.Links) == 0 {
		return
	}
	head := fmt.Sprintf("Links (%d)  L open", len(iss.Links))
	for i, l := range lines {
		if strings.TrimSpace(ansi.Strip(l)) != head {
			continue
		}
		m.panelHits[i] = panelHit{field: -1, press: "L"}
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
	if h.hints {
		h.press = panelHintAt(h.col)
	}
	switch {
	case h.image != "":
		m.openImageViewAt(h.image)
		return m, nil
	case h.double && count < 2:
		return m, nil
	case h.press != "":
		return m.handleRefKey(keyPress(h.press))
	case h.hints:
		return m, nil
	}
	if h.url != "" {
		m.status = "opening " + h.url + "…"
		return m, m.openOpenable(openable{name: h.url, url: h.url})
	}
	if h.reply {
		if h.field < len(m.jiraIssue.Comments) {
			m.openJiraReply(m.jiraIssue.Comments[h.field])
		}
		return m, nil
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

// imageOn is the attachment whose placeholder line l shows, "" for none.
func (m *Model) imageOn(l string) string {
	sm := imageFG.FindStringSubmatch(l)
	if sm == nil || m.images == nil {
		return ""
	}
	var id uint32
	for _, v := range sm[1:] {
		n, _ := strconv.Atoi(v)
		id = id<<8 | uint32(n)
	}
	for att, e := range m.images.byAtt {
		if e.id == id {
			return att
		}
	}
	return ""
}

// panelIndent is how many cells of leading space line has.
func panelIndent(line string) int {
	s := ansi.Strip(line)
	return len(s) - len(strings.TrimLeft(s, " "))
}

// panelHintAt is the key of the hint at column col of panelHintLine, ""
// between them.
func panelHintAt(col int) string {
	at := 0
	for _, h := range strings.Split(panelHintLine, " · ") {
		w := ansi.StringWidth(h)
		if col >= at && col < at+w {
			k, _, _ := strings.Cut(h, " ")
			if k == "↵" {
				return "enter"
			}
			return k
		}
		at += w + 3
	}
	return ""
}

// The panel's right border is its scrollbar, beside the body's rows from
// the row under the title (as renderRightBorder draws the thumb).

// onPanelScrollbar is whether screen row y is on the scrollbar's track and
// the body is taller than the panel.
func (m *Model) onPanelScrollbar(y int) bool {
	h := m.refView.Height()
	return y >= 1 && y < 1+h && viewportVisualRows(m.refView.GetContent(), m.refView.Width()) > h
}

// scrollPanelTo scrolls the panel so its thumb sits at row y: the top row
// is the start, the last the end.
func (m *Model) scrollPanelTo(y int) {
	h := m.refView.Height()
	total := viewportVisualRows(m.refView.GetContent(), m.refView.Width())
	if total <= h || h < 2 {
		return
	}
	f := float64(min(max(y-1, 0), h-1)) / float64(h-1)
	m.refView.SetYOffset(int(f*float64(total-h) + 0.5))
}
