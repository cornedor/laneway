package ui

import (
	"fmt"
	"github.com/cornedor/laneway/internal/i18n"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge"
)

// alt+m swaps the board for the merge requests waiting on you (MRInbox):
// a row each, grouped by why. enter reads one in the panel, as D does; d
// goes straight to its diff, o opens it in GitLab, r reads them again, esc
// goes back to the board.

type mrScreen struct {
	rows    []MRRow
	errs    []string
	row     int
	top     int // the first list line shown
	loading bool
	seq     int
}

type mrInboxMsg struct {
	seq  int
	rows []MRRow
	errs []string
}

// openMRs swaps the board for the merge requests screen.
func (m *Model) openMRs() tea.Cmd {
	if m.gitlab == nil {
		m.status = i18n.T("no GitLab: add one under gitlab: in the config, or glab auth login")
		return nil
	}
	m.jiraTab.mrs = &mrScreen{}
	m.focus = focusJira
	return m.loadMRs()
}

func (m *Model) loadMRs() tea.Cmd {
	s := m.jiraTab.mrs
	s.seq++
	s.loading = true
	sites, st, ctx, seq := m.gitlab, m.store, m.ctx, s.seq
	return func() tea.Msg {
		rows, errs := MRInbox(ctx, sites, st)
		return mrInboxMsg{seq: seq, rows: rows, errs: errs}
	}
}

func (m Model) handleMRInbox(msg mrInboxMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.mrs
	if s == nil || msg.seq != s.seq {
		return m, nil
	}
	keep := ""
	if s.row < len(s.rows) {
		keep = s.rows[s.row].MR.WebURL
	}
	s.rows, s.errs, s.loading = msg.rows, msg.errs, false
	s.row = 0
	for i, r := range s.rows {
		if r.MR.WebURL == keep {
			s.row = i
		}
	}
	m.renderJira()
	return m, nil
}

func (m *Model) mrsViewLine() string {
	s := m.jiraTab.mrs
	line := i18n.T("Merge requests waiting on you")
	switch {
	case s.loading:
		line += i18n.T(" · reading GitLab…")
	default:
		line += fmt.Sprintf(" · %d", len(s.rows))
	}
	return jiraViewActive.Render(line) + jiraDimStyle.Render("  "+i18n.Tf("%s read · d review the diff · %s GitLab · esc board", helpKey(m.keys.OpenChannel), helpKey(m.keys.OpenAttach)))
}

// mrRowLine is one merge request's row, w wide.
func mrRowLine(r MRRow, w int) string {
	mr := r.MR
	ref := mr.Repo + "!" + strconv.Itoa(mr.Number)
	var tail []string
	if mr.Draft {
		tail = append(tail, i18n.T("draft"))
	}
	if mr.Author != "" {
		tail = append(tail, mr.Author)
	}
	if mr.Notes > 0 {
		tail = append(tail, i18n.Tn(mr.Notes, "%d comment", "%d comments", mr.Notes))
	}
	if a := age(mr.UpdatedAt); a != "" {
		tail = append(tail, a)
	}
	mark := " "
	if mr.Checks != nil {
		mark = checkGlyph(mr.Checks.Status)
	}
	right := jiraDimStyle.Render(strings.Join(tail, " · "))
	left := mark + " " + jiraKeyStyle.Render(ref) + "  " + mr.Title
	room := w - ansi.StringWidth(right) - 2
	return truncate(left, max(room, 10)) + "  " + right
}

func (m *Model) renderMRs(w, h int) string {
	s := m.jiraTab.mrs
	var lines []string
	rowLine := make([]int, len(s.rows))
	for i, r := range s.rows {
		if i == 0 || r.Group != s.rows[i-1].Group {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, jiraViewActive.Render(i18n.T(r.Group)))
		}
		rowLine[i] = len(lines)
		line := mrRowLine(r, w-2)
		if i == s.row {
			line = selectedRow.Render(ansi.Strip(line))
		}
		lines = append(lines, " "+line)
	}
	switch {
	case s.loading && len(s.rows) == 0:
		lines = append(lines, jiraDimStyle.Render(i18n.T("reading the merge requests waiting on you…")))
	case len(s.rows) == 0:
		lines = append(lines, jiraDimStyle.Render(i18n.T("nothing waits on you")))
	}
	for _, e := range s.errs {
		lines = append(lines, "", refErrStyle.Render(truncate(e, w)))
	}
	if len(s.rows) > 0 {
		at := rowLine[s.row]
		s.top = min(s.top, max(at-1, 0)) // its group heading too
		for at >= s.top+h {
			s.top++
		}
	} else {
		s.top = 0
	}
	end := min(s.top+h, len(lines))
	return strings.Join(lines[min(s.top, end):end], "\n")
}

func (m Model) handleMRsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.jiraTab.mrs
	var cur *forge.Change
	if s.row < len(s.rows) {
		cur = s.rows[s.row].MR
	}
	switch {
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit):
		return m.quit()
	case msg.String() == "esc", key.Matches(msg, m.keys.MergeRequests):
		m.jiraTab.mrs = nil
		m.renderJira()
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.helpOpen = true
	case key.Matches(msg, m.keys.Refresh):
		return m, m.loadMRs()
	case key.Matches(msg, m.keys.OpenChannel), msg.String() == "d":
		if cur == nil {
			return m, nil
		}
		c, r, ok := m.gitlabMR(cur.WebURL)
		if !ok {
			return m, m.openOpenable(openable{name: cur.Title, url: cur.WebURL})
		}
		cmd := m.showMR(c, r, cur.WebURL, cur.Title)
		m.mr.thenDiff = msg.String() == "d"
		return m, cmd
	case key.Matches(msg, m.keys.OpenAttach):
		if cur == nil {
			return m, nil
		}
		m.status = i18n.Tf("opening %s…", cur.WebURL)
		return m, m.openOpenable(openable{name: cur.Title, url: cur.WebURL})
	default:
		s.row, _ = m.keys.listNav(msg, s.row, len(s.rows), max(m.jiraTab.view.Height()/2, 1), false)
	}
	m.renderJira()
	return m, nil
}
