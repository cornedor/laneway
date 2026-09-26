package ui

import (
	"encoding/json"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// Q runs a JQL search as a view, with the completions Jira's own search box
// offers: fields, functions and keywords at the start of a clause, the
// field's values after an operator. tab takes the chosen completion.

const jqlShown = 8 // completions listed at once

// jqlState is the open JQL input.
type jqlState struct {
	input textinput.Model
	words jira.JQLWords
	sugg  []string
	idx   int
	seq   int // drops a stale value lookup
}

// jqlWordsMsg and jqlValuesMsg carry completions in.
type jqlWordsMsg struct {
	words jira.JQLWords
	err   error
}

type jqlValuesMsg struct {
	seq    int
	values []string
}

// jqlOperators end a clause's field: what follows is a value.
var jqlOperators = []string{"=", "!=", "~", "!~", ">", ">=", "<", "<=", "in", "is", "was", "changed"}

// jqlContext reads what the cursor (at the end of s) is completing: a value
// of field after "field operator" (or inside "field in (…"), else a field or
// keyword. start is where the word being completed begins.
func jqlContext(s string) (field, prefix string, start int, value bool) {
	start = strings.LastIndexAny(s, " (,") + 1
	if strings.Count(s, `"`)%2 == 1 {
		start = strings.LastIndex(s, `"`) // inside a quoted word: complete it whole
	}
	prefix = strings.Trim(s[start:], `"`)
	head := s[:start]
	if open := strings.LastIndex(head, "("); open >= 0 && !strings.Contains(head[open:], ")") {
		head = head[:open] // a list: the operator stands before its "("
	}
	words := strings.Fields(strings.ReplaceAll(head, ",", " "))
	n := len(words)
	lower := func(i int) string { return strings.ToLower(words[i]) }
	isOp := func(i int) bool { return slices.Contains(jqlOperators, lower(i)) }
	switch {
	case n >= 3 && lower(n-1) == "in" && lower(n-2) == "not": // not in
		return words[n-3], prefix, start, true
	case n >= 3 && lower(n-1) == "not" && isOp(n-2): // is not, was not
		return words[n-3], prefix, start, true
	case n >= 2 && isOp(n-1):
		return words[n-2], prefix, start, true
	}
	return "", prefix, start, false
}

// jqlComplete puts word in place of the one being completed, quoted when
// it has spaces, and a space after.
func jqlComplete(s, word string) string {
	_, _, start, _ := jqlContext(s)
	if strings.ContainsAny(word, " ") && !strings.HasPrefix(word, `"`) {
		word = `"` + word + `"`
	}
	return s[:start] + word + " "
}

// jqlMatches are the words starting like prefix, then those containing it.
func jqlMatches(words []string, prefix string) []string {
	p := strings.ToLower(prefix)
	var head, rest []string
	for _, w := range words {
		lw := strings.ToLower(strings.Trim(w, `"`))
		switch {
		case strings.HasPrefix(lw, p):
			head = append(head, w)
		case p != "" && strings.Contains(lw, p):
			rest = append(rest, w)
		}
	}
	return append(head, rest...)
}

// openJQL opens the input, fetching the completion words.
func (m *Model) openJQL() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "project = ABC AND assignee = currentUser() ORDER BY updated DESC"
	ti.SetWidth(max(min(m.width-16, 90), 20))
	ti.Focus()
	m.jql = &jqlState{input: ti}
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		w, err := c.JQLAutocomplete(ctx)
		return jqlWordsMsg{w, err}
	}
}

func (m Model) handleJQLWords(msg jqlWordsMsg) (tea.Model, tea.Cmd) {
	if m.jql == nil {
		return m, nil
	}
	if msg.err != nil {
		m.status = "jql completion: " + msg.err.Error()
	}
	m.jql.words = msg.words
	return m, m.suggestJQL()
}

func (m Model) handleJQLValues(msg jqlValuesMsg) (tea.Model, tea.Cmd) {
	if m.jql == nil || msg.seq != m.jql.seq {
		return m, nil
	}
	m.jql.sugg, m.jql.idx = msg.values, 0
	return m, nil
}

// suggestJQL refreshes the completions for the input: words at once, a
// field's values from Jira.
func (m *Model) suggestJQL() tea.Cmd {
	j := m.jql
	j.seq++
	if strings.TrimSpace(j.input.Value()) == "" {
		j.sugg, j.idx = m.jqlList(jqlHistoryMeta), 0 // past searches
		return nil
	}
	field, prefix, _, value := jqlContext(j.input.Value())
	if !value {
		all := slices.Concat(j.words.Fields, j.words.Functions, j.words.Reserved)
		j.sugg, j.idx = jqlMatches(all, prefix), 0
		return nil
	}
	fn := jqlMatches(j.words.Functions, prefix)
	seq, c, ctx := j.seq, m.jiraClient, m.ctx
	return func() tea.Msg {
		vals, _ := c.JQLValues(ctx, field, prefix)
		return jqlValuesMsg{seq, append(vals, fn...)}
	}
}

func (m Model) handleJQLKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	j := m.jql
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.jql = nil
		return m, nil
	case "up", "ctrl+p":
		j.idx = max(j.idx-1, 0)
		return m, nil
	case "down", "ctrl+n":
		j.idx = min(j.idx+1, max(len(j.sugg)-1, 0))
		return m, nil
	case "ctrl+s":
		if q := strings.TrimSpace(j.input.Value()); q != "" {
			m.toggleSavedJQL(q)
		}
		return m, nil
	case "tab":
		if strings.TrimSpace(j.input.Value()) == "" && j.idx < len(j.sugg) {
			j.input.SetValue(j.sugg[j.idx]) // a past search, whole
			j.input.CursorEnd()
			return m, m.suggestJQL()
		}
		if j.idx < len(j.sugg) {
			j.input.SetValue(jqlComplete(j.input.Value(), j.sugg[j.idx]))
			j.input.CursorEnd()
			return m, m.suggestJQL()
		}
		return m, nil
	case "enter":
		q := strings.TrimSpace(j.input.Value())
		m.jql = nil
		if q == "" {
			return m, nil
		}
		m.rememberJQL(q)
		return m, m.runJQLView(q)
	}
	before := j.input.Value()
	var cmd tea.Cmd
	j.input, cmd = j.input.Update(msg)
	if j.input.Value() == before {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.suggestJQL())
}

// runJQLView shows q's results as a view of their own, replacing an
// earlier search's.
func (m *Model) runJQLView(q string) tea.Cmd {
	return m.runNamedJQLView("JQL: "+ansi.Truncate(q, 30, "…"), q)
}

// runNamedJQLView shows q's results as a view named name, replacing an
// earlier one of the same kind (the part of name before ": ").
func (m *Model) runNamedJQLView(name, q string) tea.Cmd {
	t := m.jiraTab
	if t.cfg == nil {
		m.status = "open a board first"
		return nil
	}
	v := jiraView{kind: jiraViewFilter, name: name, jql: q}
	prefix, _, _ := strings.Cut(name, ": ")
	i := slices.IndexFunc(t.views, func(v jiraView) bool {
		return v.kind == jiraViewFilter && strings.HasPrefix(v.name, prefix+": ")
	})
	if i < 0 {
		t.views = append(t.views, v)
		i = len(t.views) - 1
	} else {
		t.views[i] = v
	}
	return m.loadJiraCards(i, false)
}

// myWorkJQL is everything assigned to you anywhere, open or done this week.
const myWorkJQL = "assignee = currentUser() AND (statusCategory != Done OR resolved >= -7d) ORDER BY updated DESC"

// openMyWork shows your issues across boards and projects as a view,
// grouped by status.
func (m *Model) openMyWork() tea.Cmd {
	cmd := m.runNamedJQLView("Mine: my work", myWorkJQL)
	if cmd != nil {
		m.jiraTab.sort = jiraSortStatus
	}
	return cmd
}

// top is the first completion shown.
func (j *jqlState) top() int {
	return max(0, min(j.idx-jqlShown+1, len(j.sugg)-jqlShown))
}

// renderJQL draws the input and its completions as a modal.
func (m *Model) renderJQL() string {
	j := m.jql
	inner := j.input.Width() + 2
	lines := []string{lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Bold(true).Render("JQL search"), "", j.input.View(), ""}
	top := j.top()
	for i := top; i < len(j.sugg) && i < top+jqlShown; i++ {
		s := ansi.Truncate(j.sugg[i], inner-2, "…")
		if i == j.idx {
			lines = append(lines, lipgloss.NewStyle().Foreground(focusedColor).Bold(true).Render("▸ "+s))
		} else {
			lines = append(lines, "  "+refDimStyle.Render(s))
		}
	}
	if len(j.sugg) == 0 {
		lines = append(lines, refDimStyle.Render("  no completions"))
	}
	hint := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Foreground(dimColor).Italic(true).
		Render("tab complete · ↑↓ choose · ↵ search · ctrl+s star as a view · esc cancel")
	lines = append(lines, "", hint)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).
		Render(strings.Join(lines, "\n"))
}

// Past and starred searches live in the store: the history as completions
// for an empty input, the starred ones as views of every board.
const (
	jqlHistoryMeta = jiraMetaPrefix + "jql_history"
	jqlSavedMeta   = jiraMetaPrefix + "jql_saved"
	jqlHistoryMax  = 20
)

func (m *Model) jqlList(meta string) []string {
	if m.store == nil {
		return nil
	}
	v, ok, _ := m.store.GetMeta(meta)
	var out []string
	if ok {
		_ = json.Unmarshal([]byte(v), &out)
	}
	return out
}

func (m *Model) setJQLList(meta string, list []string) {
	if m.store == nil {
		return
	}
	b, _ := json.Marshal(list)
	_ = m.store.SetMeta(meta, string(b))
}

// rememberJQL puts q first in the history.
func (m *Model) rememberJQL(q string) {
	h := slices.DeleteFunc(m.jqlList(jqlHistoryMeta), func(s string) bool { return s == q })
	h = append([]string{q}, h...)
	m.setJQLList(jqlHistoryMeta, h[:min(len(h), jqlHistoryMax)])
}

// toggleSavedJQL stars q as a view of every board, or unstars it.
func (m *Model) toggleSavedJQL(q string) {
	saved := m.jqlList(jqlSavedMeta)
	if i := slices.Index(saved, q); i >= 0 {
		m.setJQLList(jqlSavedMeta, slices.Delete(saved, i, i+1))
		m.status = "unstarred; its view goes on the next board load"
		return
	}
	m.setJQLList(jqlSavedMeta, append(saved, q))
	m.status = "starred as a view of every board"
}

// savedJQLViews are the starred searches as views.
func (m *Model) savedJQLViews() []jiraView {
	var out []jiraView
	for _, q := range m.jqlList(jqlSavedMeta) {
		out = append(out, jiraView{kind: jiraViewFilter, name: "★ " + ansi.Truncate(q, 30, "…"), jql: q})
	}
	return out
}
