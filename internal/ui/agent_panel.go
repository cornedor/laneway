package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// With ui.agent_view: panel, attaching to an agent (S, the palette) runs
// herdr agent attach in the panel instead of taking the whole screen: the
// agent's terminal under a strip for its issue. While the panel has focus
// every key goes to the agent except agent_back (ctrl+\), which, like a
// click on the strip, goes back to the issue. The wheel, clicks and drags
// reach the agent when it takes the mouse. Detaching inside it (herdr's
// ctrl+b q) closes it too; the agent keeps running either way.

// openAgentPanel shows key's agent in pane in the panel, attached through
// bin.
func (m *Model) openAgentPanel(key, pane, bin string) tea.Cmd {
	var cmd tea.Cmd
	if iss := m.jiraIssue; !m.refOpen || iss == nil || iss.Key != key {
		m.selectJiraKey(key)
		out, c := m.openJiraKey(key)
		*m, cmd = out.(Model), c
	}
	m.closeAgentPanel()
	title := pane
	if a, ok := m.agentByPane(key, pane); ok && a.Name != "" {
		title = a.Name
	}
	w, h := m.agentTermSize()
	t, err := startTerm(termSpec{
		title: title,
		argv:  []string{bin, "agent", "attach", pane},
		env:   []string{"HERDR_SOCKET_PATH=" + m.herdr.Path()},
	}, w, h)
	if err != nil {
		m.fail(key + ": attach: " + err.Error())
		return cmd
	}
	m.agentTerm, m.agentTermKey = t, key
	m.focus = focusRef
	m.status = key + ": attached · " + helpKey(m.keys.AgentBack) + " back to the issue"
	return tea.Batch(cmd, waitTermOutput(t))
}

// closeAgentPanel detaches the panel's terminal; the agent keeps running.
func (m *Model) closeAgentPanel() {
	if m.agentTerm == nil {
		return
	}
	m.agentTerm.stop()
	m.agentTerm, m.agentTermKey, m.agentTermDrag = nil, "", false
}

// agentTermShown is whether the panel shows an agent's terminal.
func (m *Model) agentTermShown() bool {
	return m.agentTerm != nil && m.refOpen
}

// agentTermSize is the terminal's width and height in the panel: under the
// title and the issue's strip, above the bottom border.
func (m *Model) agentTermSize() (w, h int) {
	return max(m.refView.Width(), 10), max(m.bodyH()-3, 3)
}

// agentTermOrigin is the screen cell of the terminal's top-left.
func (m *Model) agentTermOrigin() (x, y int) {
	listW, _ := m.jiraListWidth(m.width)
	return listW + 1, 2
}

// agentTermCell is the terminal cell under screen (x, y).
func (m *Model) agentTermCell(x, y int) (cx, cy int, ok bool) {
	t := m.agentTerm
	x0, y0 := m.agentTermOrigin()
	cx, cy = x-x0, y-y0
	return cx, cy, t != nil && cx >= 0 && cy >= 0 && cx < t.w && cy < t.h
}

func (m Model) handleAgentTermKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.agentTerm
	if key.Matches(msg, m.keys.AgentBack) || t.exited() && (msg.String() == "esc" || msg.String() == "enter") {
		return m.leaveAgentPanel()
	}
	t.sendKey(msg)
	return m, nil
}

// leaveAgentPanel closes the terminal and shows its issue again.
func (m Model) leaveAgentPanel() (tea.Model, tea.Cmd) {
	key := m.agentTermKey
	m.closeAgentPanel()
	m.status = key + ": back from its agent"
	m.renderRef()
	return m, m.fetchAgents()
}

func (m Model) handleTermOutput(t *termSession, exited bool) (tea.Model, tea.Cmd) {
	if t != m.agentTerm {
		return m, nil // one closed already
	}
	if exited {
		failed := t.exitErr != nil
		why := t.exitStatus() + lastLine(t.view())
		out, cmd := m.leaveAgentPanel()
		if om := out.(Model); failed {
			om.fail(om.status + " · attach " + why)
			return om, cmd
		}
		return out, cmd
	}
	return m, waitTermOutput(t)
}

// clickAgentPanel handles a click in the panel while it shows a terminal:
// the strip goes back to the issue, the terminal takes focus and, when the
// agent takes the mouse, the press.
func (m Model) clickAgentPanel(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Y == 1 {
		return m.leaveAgentPanel()
	}
	m.focus = focusRef
	if x, y, ok := m.agentTermCell(msg.X, msg.Y); ok && m.agentTerm.wantsMouse() {
		m.agentTermDrag = m.agentTerm.mouseEvent(termButton(msg.Mouse(), false), x, y, false)
	}
	return m, nil
}

// agentTermDragTo forwards motion with the button held, or the release, of
// a drag that began in the terminal, clamped to it.
func (m *Model) agentTermDragTo(ms tea.Mouse, release bool) {
	t := m.agentTerm
	if t == nil {
		m.agentTermDrag = false
		return
	}
	if release || t.wantsDrag() {
		x0, y0 := m.agentTermOrigin()
		x := min(max(ms.X-x0, 0), t.w-1)
		y := min(max(ms.Y-y0, 0), t.h-1)
		t.mouseEvent(termButton(ms, !release), x, y, release)
	}
	if release {
		m.agentTermDrag = false
	}
}

// agentTermWheel hands a wheel notch over the terminal to the agent.
func (m *Model) agentTermWheel(msg tea.MouseWheelMsg) bool {
	x, y, ok := m.agentTermCell(msg.X, msg.Y)
	if !ok {
		return false
	}
	switch msg.Button {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		m.agentTerm.wheel(msg.Button == tea.MouseWheelUp, x, y)
	}
	return true
}

// agentTermCursor is where the focused terminal's cursor sits on screen:
// agents like Claude Code draw no caret, they park the real cursor.
func (m *Model) agentTermCursor() (x, y int, ok bool) {
	t := m.agentTerm
	if !m.agentTermShown() || m.focus != focusRef || m.modalOpen() || t.exited() || t.cursorHidden.Load() {
		return 0, 0, false
	}
	p := t.emu.CursorPosition()
	if p.X < 0 || p.Y < 0 || p.X >= t.w || p.Y >= t.h {
		return 0, 0, false
	}
	x0, y0 := m.agentTermOrigin()
	return x0 + p.X, y0 + p.Y, true
}

// renderAgentPane draws the panel with the terminal: the title, the
// issue's strip, the screen.
func (m *Model) renderAgentPane(height, width int) string {
	innerH := max(height, 1)
	width = max(width, refPaneMinWidth)
	t := m.agentTerm
	title := "Agent  " + t.title
	switch {
	case t.exited():
		title += " · " + t.exitStatus() + " · esc back"
	case m.focus == focusRef:
		title += " · " + helpKey(m.keys.AgentBack) + " back"
	default:
		title += " · click to type"
	}
	strip := refKeyStyle.Render("↰ " + m.agentTermKey)
	if iss := m.jiraIssue; iss != nil && iss.Key == m.agentTermKey {
		strip += "  " + refDimStyle.Render(iss.Status) + "  " + iss.Summary
	}
	innerW := width - 3
	rows := []string{bar(titleStyle.Render(ansi.Truncate(title, innerW, "…")), innerW), bar(ansi.Truncate(strip, innerW, "…"), innerW)}
	lines := strings.Split(t.view(), "\n")
	for i := 0; len(rows) < innerH-1; i++ {
		row := ""
		if i < len(lines) {
			row = ansi.Truncate(lines[i], innerW, "")
		}
		rows = append(rows, row)
	}
	borderColor := dimColor
	if m.focus == focusRef {
		borderColor = focusedColor
	}
	style := lipgloss.NewStyle().Border(border).UnsetBorderTop().UnsetBorderRight().
		Width(width - 1).Height(innerH).BorderForeground(borderColor)
	box := style.Render(strings.Join(rows, "\n"))
	rightBorder := renderRightBorder(innerH, 1, innerH-1, 0, 0, borderColor, false, -1)
	return lipgloss.JoinHorizontal(lipgloss.Top, box, rightBorder)
}

func termButton(ms tea.Mouse, motion bool) byte {
	return ansi.EncodeMouseButton(ms.Button, motion, ms.Mod&tea.ModShift != 0, ms.Mod&tea.ModAlt != 0, ms.Mod&tea.ModCtrl != 0)
}

// lastLine is the screen's last line with text, as ": text", "" for none:
// what herdr said when the attach failed.
func lastLine(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(ansi.Strip(lines[i])); l != "" {
			return ": " + l
		}
	}
	return ""
}
