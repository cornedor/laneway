package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// screenAt is where text first shows on m's screen: its row and column.
func screenAt(t *testing.T, m Model, text string) (x, y int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("no %q on screen", text)
	return 0, 0
}

// hovered is the text hover underlines at x, y.
func hovered(m Model, x, y int) string {
	h := m.hoverAt(x, y)
	if h.x1 <= h.x0 {
		return ""
	}
	row := strings.Split(m.View().Content, "\n")[h.y]
	return strings.TrimSpace(ansi.Strip(ansi.Cut(row, h.x0, h.x1)))
}

func TestHoverHeaderHint(t *testing.T) {
	m := jiraTabModel(t)
	x, y := screenAt(t, m, "p project")
	if got := hovered(m, x+2, y); got != "p project" {
		t.Errorf("hover over the hint = %q, want %q", got, "p project")
	}
	m.mouseX, m.mouseY, m.mouseIn = x+2, y, true
	row := strings.Split(m.View().Content, "\n")[y]
	if !strings.Contains(row, "\x1b[4m") || ansi.Strip(row) != strings.Split(ansi.Strip(jiraTabModel(t).View().Content), "\n")[y] {
		t.Errorf("underlined row changed its text or has no underline: %q", row)
	}
}

// Over the panel's hint row, hover lights exactly the cells a click presses
// a key on.
func TestHoverMatchesClick(t *testing.T) {
	m := loadedJiraModel(t)
	m.width = 200 // the whole hint row on one line
	m.resize()
	m.renderRef()
	x0, y := screenAt(t, m, "tab fields")
	listW, _ := m.jiraListWidth(m.width)
	lit := 0
	defer func() {
		if lit == 0 {
			t.Error("no hint lit")
		}
	}()
	for x := x0; x < m.width-1; x++ {
		col := x - listW - 1 - panelIndent(strings.Split(m.refView.GetContent(), "\n")[m.panelLineAt(y)])
		key := m.panelHintAt(col)
		on := hovered(m, x, y) != ""
		if on != (key != "") {
			t.Fatalf("column %d: hover lit %v, click presses %q", x, on, key)
		}
		if on {
			lit++
		}
	}
}

func TestHoverPointer(t *testing.T) {
	m := jiraTabModel(t)
	m.pointerOn = true
	move := func(x, y int) tea.Cmd {
		t.Helper()
		out, cmd := m.Update(tea.MouseMotionMsg{X: x, Y: y})
		m = out.(Model)
		return cmd
	}
	x, y := screenAt(t, m, "p project")
	if move(x, y) == nil || m.pointer != pointerHand {
		t.Fatalf("over a hint the pointer is %q, want a hand", m.pointer)
	}
	if move(x+1, y) != nil {
		t.Error("moving on the same target set the pointer again")
	}
	if move(0, m.bodyH()) == nil || m.pointer != "default" {
		t.Errorf("off it the pointer is %q, want default", m.pointer)
	}
	if got := m.ReleasePointer(); got != "" {
		t.Errorf("release at the default = %q, want none", got)
	}
}
