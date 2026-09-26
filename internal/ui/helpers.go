package ui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/emoji"
	"github.com/cornedor/laneway/internal/opener"
	"github.com/cornedor/laneway/internal/textwidth"
)

// Helpers and styles lifted from matterbox's view/model code.

const (
	maxInputHeight        = 6
	confirmDialogMaxWidth = 60
	pickerMaxWidth        = 100
	refPaneMinWidth       = 24
	// diffSoftReset clears text attributes but keeps the background.
	diffSoftReset = "\x1b[22;23;24;39m"
)

var (
	border          = lipgloss.NormalBorder()
	titleStyle      = lipgloss.NewStyle().Bold(true)
	gitlabWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// Themed colours and styles, set by applyTheme (theme.go).
var (
	focusedColor, dimColor color.Color

	selectedRow, diffTreeSelStyle, scrollbarThumbStyle lipgloss.Style
	mentionStyle, attachmentStyle, statusStyle         lipgloss.Style
)

// emojiImages resolves custom server emoji in matterbox; Jira has none, so
// renderMarkdown always gets nil.
type emojiImages struct{}

func (*emojiImages) inline(string) (string, bool) { return "", false }

func unicodeEmojiGlyph(name string) string { return emoji.Glyph(name) }

func helpKey(b key.Binding) string { return b.Help().Key }

// keepBG rewrites every full SGR reset inside s to diffSoftReset and the
// row's own style (open, its SGR), so a row's background survives a styled
// span, and one with a background of its own (an avatar chip) ends.
func keepBG(s, open string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	s = strings.ReplaceAll(s, "\x1b[0m", diffSoftReset+open)
	return strings.ReplaceAll(s, "\x1b[m", diffSoftReset+open)
}

func visualWidth(s string) int { return textwidth.Width(s) }

// visualRowsBefore counts the soft-wrapped rows of lines[:n] at maxWidth.
func visualRowsBefore(lines []string, n, maxWidth int) int {
	n = min(n, len(lines))
	if n <= 0 {
		return 0
	}
	if maxWidth <= 0 {
		return n
	}
	rows := 0
	for i := 0; i < n; i++ {
		w := visualWidth(lines[i])
		if w <= maxWidth {
			rows++
			continue
		}
		rows += (w + maxWidth - 1) / maxWidth
	}
	return rows
}

func viewportVisualRows(content string, width int) int {
	if content == "" {
		return 0
	}
	lines := strings.Split(content, "\n")
	return visualRowsBefore(lines, len(lines), width)
}

func scrollPercentFor(total, height, yOffset int) float64 {
	if height >= total {
		return 1.0
	}
	v := float64(yOffset) / (float64(total) - float64(height))
	return min(1.0, max(0.0, v))
}

// renderRightBorder draws a pane's right edge, with a scrollbar thumb over
// the viewport rows when showScrollbar is set.
func renderRightBorder(outerH, vpTop, vpHeight int, totalRows int, percent float64, borderColor color.Color, showScrollbar bool, joinRow int) string {
	outerH = max(outerH, 1)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	track := borderStyle.Render("│")

	lines := make([]string, outerH)
	for i := 0; i < outerH-1; i++ {
		lines[i] = track
	}
	if joinRow >= 0 && joinRow < outerH-1 {
		lines[joinRow] = borderStyle.Render("┤")
	}
	lines[outerH-1] = borderStyle.Render("┘")

	if showScrollbar && vpHeight > 0 && totalRows > vpHeight {
		thumb := min(max(vpHeight*vpHeight/totalRows, 1), vpHeight)
		avail := vpHeight - thumb
		pos := min(max(int(float64(avail)*percent+0.5), 0), avail)
		thumbCell := scrollbarThumbStyle.Render("█")
		for i := pos; i < pos+thumb; i++ {
			idx := vpTop + i
			if idx >= 0 && idx < outerH-1 {
				lines[idx] = thumbCell
			}
		}
	}
	return strings.Join(lines, "\n")
}

// joinRuleLine turns a box line holding a horizontal rule into a divider that
// meets the frame (├ … ┤).
func joinRuleLine(line string) string {
	if i := strings.Index(line, "│"); i >= 0 {
		line = line[:i] + "├" + line[i+len("│"):]
	}
	if i := strings.LastIndex(line, "│"); i >= 0 {
		line = line[:i] + "┤" + line[i+len("│"):]
	}
	return line
}

func joinRuleRows(block string, rows ...int) string {
	lines := strings.Split(block, "\n")
	touched := false
	for _, row := range rows {
		if row >= 0 && row < len(lines) {
			lines[row] = joinRuleLine(lines[row])
			touched = true
		}
	}
	if !touched {
		return block
	}
	return strings.Join(lines, "\n")
}

// splitRightPane is the issue panel's width in a body width wide: half,
// clamped so neither side drops below refPaneMinWidth.
func splitRightPane(width, pct int) int {
	return min(max(width*pct/100, refPaneMinWidth), max(width-refPaneMinWidth, refPaneMinWidth))
}

// age is a compact "how long ago": 5m, 3h, 2d, 6w.
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 14*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
	return strconv.Itoa(int(d.Hours()/24/7)) + "w"
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	budget := n - 1 // reserve one cell for the ellipsis
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > budget {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// placeOffset is the leading pad lipgloss.Place puts before a box of size box
// centred in total.
func placeOffset(total, box int) int {
	return max(total-box, 0) / 2
}

// inputPromptFunc renders prompt on the first visual line and pads
// continuation lines with two spaces.
func inputPromptFunc(prompt string) func(visualLine int, focused bool) string {
	return func(visualLine int, _ bool) string {
		if visualLine == 0 {
			return prompt
		}
		return "  "
	}
}

// expandUserPath expands a leading "~" (or "~/") in p to the home directory.
func expandUserPath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// openable is a URL o hands to the OS default handler.
type openable struct {
	name string
	url  string
}

type openedMsg struct {
	name string
	err  error
}

func (m Model) openOpenable(o openable) tea.Cmd {
	command := m.opts.openCmd
	return func() tea.Msg {
		return openedMsg{name: o.name, err: opener.Open(command, o.url)}
	}
}

// when is a panel date: relative within a week ("just now", "5m ago", "3h
// ago", "2d ago"), else in ui.date_format; zero is "".
func (m *Model) when(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return relativeDate(t, time.Now(), m.opts.dateFormat)
}

func relativeDate(t, now time.Time, layout string) string {
	d := now.Sub(t)
	switch {
	case d < 0 || d >= 7*24*time.Hour:
		return t.Local().Format(layout)
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
