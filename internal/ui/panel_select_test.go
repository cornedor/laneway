package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestPanelSelect: a drag over the panel's description selects the text
// under it, drawn reversed, and letting go copies it without the gutter.
func TestPanelSelect(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: &jira.Issue{Key: "ABC-1",
		Description: "first line of text\n\nsecond line here"}})
	m = out.(Model)
	lines := strings.Split(m.refView.GetContent(), "\n")
	at := func(s string) (x, y int) {
		t.Helper()
		for i, l := range lines {
			if c := strings.Index(ansi.Strip(l), s); c >= 0 {
				listW, _ := m.jiraListWidth(m.width)
				return listW + 1 + ansi.StringWidth(ansi.Strip(l)[:c]), 1 + m.crumbRows() + visualRowsBefore(lines, i, m.refView.Width()) - m.refView.YOffset()
			}
		}
		t.Fatalf("no %q:\n%s", s, ansi.Strip(m.refView.GetContent()))
		return 0, 0
	}
	x1, y1 := at("line of text")
	x2, y2 := at("here")
	out, _ = m.Update(tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	out, _ = out.(Model).Update(tea.MouseMotionMsg{X: x2 + 3, Y: y2, Button: tea.MouseLeft})
	m = out.(Model)
	if !m.panelSel.on || !strings.Contains(m.refView.GetContent(), panelSelStyle.Render("line of text")) {
		t.Fatalf("selection not drawn: %+v\n%q", m.panelSel, m.refView.GetContent())
	}
	out, cmd := m.Update(tea.MouseReleaseMsg{X: x2 + 3, Y: y2, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil || m.status != "copied 30 characters" {
		t.Fatalf("status %q", m.status)
	}
	if got := m.panelSelText(); got != "line of text\n\nsecond line here" {
		t.Errorf("text = %q", got)
	}

	// A click without a drag clears it and copies nothing.
	out, _ = m.Update(tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	out, cmd = out.(Model).Update(tea.MouseReleaseMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	if m = out.(Model); m.panelSel.on || cmd != nil || strings.Contains(m.refView.GetContent(), panelSelStyle.Render("line of text")) {
		t.Errorf("a click kept the selection: %+v", m.panelSel)
	}
}

// TestPanelSelectWrapped: a paragraph the panel wraps copies as the one
// line it is, its words run on over each break.
func TestPanelSelectWrapped(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	para := strings.TrimSpace(strings.Repeat("words that wrap around ", 12))
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: &jira.Issue{Key: "ABC-1", Description: para}})
	m = out.(Model)
	first := slices.IndexFunc(m.panelPlain, func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "words that") })
	last := first
	for last >= 0 && m.panelSoft[last] {
		last++
	}
	if first < 0 || last == first {
		t.Fatalf("paragraph not wrapped:\n%s", strings.Join(m.panelPlain, "\n"))
	}
	m.panelSel = panelSel{on: true, a: [2]int{first, 0}, b: [2]int{last, 999}}
	if got := m.panelSelText(); got != para {
		t.Errorf("text = %q, want %q", got, para)
	}
}
