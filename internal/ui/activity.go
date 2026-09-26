package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// The panel's Activity section, tabbed like Jira's: Comments · History ·
// Work log · All. [ and ] (or a click on a tab) switch; the tab is kept
// from issue to issue. History and work log load when first shown.

const (
	activityComments = iota
	activityHistory
	activityWorklog
	activityAll
	activityTabs
)

// activityState is the changelog and worklogs of key, for the tabs that
// show them.
type activityState struct {
	key     string
	loading bool
	err     error
	changes []jira.InboxEntry
	logs    []jira.Worklog
}

// commentHead is a drawn comment's byline, in drawing order: a click on it
// replies to comment i.
type commentHead struct {
	text string
	i    int
}

type activityLoadedMsg struct {
	key     string
	changes []jira.InboxEntry
	logs    []jira.Worklog
	err     error
}

// loadActivity fetches the shown issue's history and worklogs when the tab
// needs them and they aren't there yet.
func (m *Model) loadActivity() tea.Cmd {
	iss := m.jiraIssue
	if m.activityTab == activityComments || iss == nil || m.activity.key == iss.Key {
		return nil
	}
	key := iss.Key
	m.activity = activityState{key: key, loading: true}
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		changes, err := c.Changelog(ctx, key)
		if err != nil {
			return activityLoadedMsg{key: key, err: err}
		}
		logs, err := c.IssueWorklogs(ctx, key)
		return activityLoadedMsg{key: key, changes: changes, logs: logs, err: err}
	}
}

func (m Model) handleActivityLoaded(msg activityLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.key != m.activity.key {
		return m, nil
	}
	m.activity = activityState{key: msg.key, err: msg.err, changes: msg.changes, logs: msg.logs}
	m.renderRef()
	return m, nil
}

// switchActivity shows tab t, scrolling the section into view.
func (m *Model) switchActivity(t int) tea.Cmd {
	if m.jiraIssue == nil {
		return nil
	}
	m.activityTab = (t%activityTabs + activityTabs) % activityTabs
	cmd := m.loadActivity()
	m.renderRef()
	if m.activityLine >= 0 {
		lines := strings.Split(m.refView.GetContent(), "\n")
		row := visualRowsBefore(lines, m.activityLine, m.refView.Width())
		if top := m.refView.YOffset(); row < top || row >= top+m.refView.Height() {
			m.refView.SetYOffset(row)
		}
	}
	return cmd
}

// activityLabels are the tabs' names; comments counts them.
func activityLabels(comments int) [activityTabs]string {
	return [activityTabs]string{fmt.Sprintf("Comments (%d)", comments), "History", "Work log", "All"}
}

// activityTabAt is the tab under display column col of the tab row line,
// -1 for none.
func activityTabAt(line string, col, comments int) int {
	plain, from := ansi.Strip(line), 0
	for t, l := range activityLabels(comments) {
		i := strings.Index(plain[from:], l)
		if i < 0 {
			return -1
		}
		start := ansi.StringWidth(plain[:from+i])
		if col >= start && col < start+ansi.StringWidth(l) {
			return t
		}
		from += i + len(l)
	}
	return -1
}

// renderJiraActivity appends the Activity section: its tab row, then the
// open tab.
func (m *Model) renderJiraActivity(b *strings.Builder, iss *jira.Issue, width int) {
	m.commentHeads = m.commentHeads[:0]
	count := max(iss.CommentTotal, len(iss.Comments))
	var tabs []string
	for t, l := range activityLabels(count) {
		st := refDimStyle
		if t == m.activityTab {
			st = refKeyStyle
		}
		tabs = append(tabs, st.Render(l))
	}
	b.WriteString(sectionHead(strings.Join(tabs, "  "), "   [ ]", width) + "\n")

	// The composer goes under the comment it replies to, else at the end.
	// A comment's edit off the Comments tab goes there too.
	defer func() {
		mark := commentMark
		switch {
		case m.descEditInline() && m.descEdit.comment != "":
			mark = descEditMark
		case !m.commentInline():
			return
		}
		if !strings.Contains(b.String(), mark) {
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n" + m.commentMarkLine(mark, 0))
		}
	}()
	if m.activityTab == activityComments {
		m.renderJiraComments(b, iss)
		return
	}
	a := m.activity
	switch {
	case a.key != iss.Key || a.loading:
		b.WriteString(refDimStyle.Render("loading…") + "\n")
		return
	case a.err != nil:
		b.WriteString(refErrStyle.Render(a.err.Error()) + "\n")
		return
	}
	switch m.activityTab {
	case activityHistory:
		if len(a.changes) == 0 {
			b.WriteString(refDimStyle.Render("no changes yet") + "\n")
		}
		for i, e := range a.changes {
			if i > 0 {
				b.WriteString("\n")
			}
			m.renderChange(b, e)
		}
	case activityWorklog:
		if len(a.logs) == 0 {
			b.WriteString(refDimStyle.Render("no work logged") + "\n")
			return
		}
		total := 0
		for i, w := range a.logs {
			if i > 0 {
				b.WriteString("\n")
			}
			m.renderWorklog(b, w)
			total += w.Seconds
		}
		b.WriteString("\n" + refDimStyle.Render(jira.FormatDuration(total)+" in all") + "\n")
	case activityAll:
		m.renderActivityAll(b, iss)
	}
}

// renderChange writes one changelog entry: who and when, a line per field;
// a multi-line field (the description) as the lines it changed.
func (m *Model) renderChange(b *strings.Builder, e jira.InboxEntry) {
	b.WriteString(refDimStyle.Render(orDash(e.Who)+" · "+m.when(e.When)) + "\n")
	if len(e.Changes) > 0 {
		for _, c := range e.Changes {
			if !strings.Contains(c.From, "\n") && !strings.Contains(c.To, "\n") {
				b.WriteString(refDimStyle.Render(c.Field+" ") + orDash(c.From) + " → " + orDash(c.To) + "\n")
				continue
			}
			b.WriteString(refDimStyle.Render(c.Field+" changed") + "\n")
			for _, l := range lineDiff(c.From, c.To, diffMaxLines) {
				switch l[0] {
				case '-':
					b.WriteString(refErrStyle.Render(l) + "\n")
				case '+':
					b.WriteString(roadmapDoneStyle.Render(l) + "\n")
				default:
					b.WriteString(refDimStyle.Render(l) + "\n")
				}
			}
		}
		return
	}
	for _, part := range strings.Split(e.What, " · ") {
		field, change, ok := strings.Cut(part, ": ")
		if !ok {
			b.WriteString(part + "\n")
			continue
		}
		b.WriteString(refDimStyle.Render(field+" ") + change + "\n")
	}
}

// diffMaxLines caps the lines a changed description shows.
const diffMaxLines = 12

// lineDiff is the lines from and to differ in, "- old" and "+ new" in
// order (a longest common subsequence apart), at most limit of them with a
// "… n more" after.
func lineDiff(from, to string, limit int) []string {
	a, b := strings.Split(from, "\n"), strings.Split(to, "\n")
	// lcs[i][j] is the common run of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	if len(out) > limit {
		out = append(out[:limit], fmt.Sprintf("  … %d more", len(out)-limit))
	}
	return out
}

// renderWorklog writes one worklog: who logged how long and when, its
// comment.
func (m *Model) renderWorklog(b *strings.Builder, w jira.Worklog) {
	b.WriteString(refDimStyle.Render(orDash(w.Author)+" · "+m.when(w.Started)) + "\n")
	b.WriteString("logged " + refKeyStyle.Render(jira.FormatDuration(w.Seconds)) + "\n")
	if w.Comment != "" {
		b.WriteString(renderMarkdown(w.Comment, m.emojiImg, nil, ""))
	}
}

// renderActivityAll writes comments, changes and worklogs as one list,
// oldest first.
func (m *Model) renderActivityAll(b *strings.Builder, iss *jira.Issue) {
	type item struct {
		at   int64
		draw func()
	}
	var items []item
	for i, c := range iss.Comments {
		items = append(items, item{c.Created.UnixNano(), func() {
			m.renderComment(b, c)
			m.commentHeads = append(m.commentHeads, commentHead{i: i, text: m.commentByline(c)})
		}})
	}
	for _, e := range m.activity.changes {
		items = append(items, item{e.When.UnixNano(), func() { m.renderChange(b, e) }})
	}
	for _, w := range m.activity.logs {
		items = append(items, item{w.Started.UnixNano(), func() { m.renderWorklog(b, w) }})
	}
	slices.SortStableFunc(items, func(x, y item) int { return cmp.Compare(x.at, y.at) })
	if len(items) == 0 {
		b.WriteString(refDimStyle.Render("nothing yet") + "\n")
	}
	for i, it := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		it.draw()
	}
}
