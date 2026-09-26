package ui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// The board's search: / narrows the loaded cards by text and field terms
// (see jiraParseQuery). It filters locally, without a refetch.

// startJiraSearch focuses the search box, keeping any query already there.
func (m *Model) startJiraSearch() {
	t := m.jiraTab
	if !t.searching {
		q := t.search.Value()
		t.search = textinput.New()
		t.search.Prompt = "/"
		t.search.Placeholder = "text, status:review points>2 prio>=high -label:ui"
		t.search.SetWidth(40)
		t.search.SetValue(q)
		t.search.CursorEnd()
	}
	t.searching = true
	t.search.Focus()
	m.renderJira()
}

// jiraSearchQuery is the active query, lowercased.
func (t *jiraTabState) jiraSearchQuery() string {
	return strings.ToLower(strings.TrimSpace(t.search.Value()))
}

// jiraCardMatches reports whether c has every term of a parsed query.
func jiraCardMatches(c jira.Card, terms []jiraTerm) bool {
	for _, t := range terms {
		if t.match(c) == t.not {
			return false
		}
	}
	return true
}

// The query language, all on the loaded cards:
//
//	login              text in key, summary, assignee or epic
//	"log in"           a phrase
//	status:review,test a field containing any of the values
//	epic:              a field that is empty
//	points>2 prio>=high comparisons: numbers, priorities by rank
//	is:flagged         flagged, done, pr, unassigned
//	-label:ui          any term negated

// jiraTerm is one term of a query.
type jiraTerm struct {
	not    bool
	field  string   // "" for text
	op     string   // ":", ">", ">=", "<", "<=", "="
	values []string // lowercased; empty for "field is empty"
}

// jiraQueryFields maps each field name (and alias) to its canonical name.
var jiraQueryFields = map[string]string{
	"status": "status", "assignee": "assignee", "who": "assignee", "type": "type",
	"prio": "priority", "priority": "priority", "epic": "parent", "parent": "parent",
	"label": "label", "labels": "label", "key": "key", "points": "points", "sp": "points", "is": "is",
}

// jiraParseQuery splits q into terms. A word that doesn't parse as a field
// term is text, so a stray colon never hides every card.
func jiraParseQuery(q string) []jiraTerm {
	var out []jiraTerm
	for _, w := range jiraQueryWords(q) {
		t := jiraTerm{}
		if strings.HasPrefix(w, "-") && len(w) > 1 {
			t.not, w = true, w[1:]
		}
		if f, op, v, ok := jiraSplitTerm(w); ok {
			t.field, t.op = f, op
			if v != "" {
				for _, part := range strings.Split(v, ",") {
					if part = strings.Trim(part, `"`); part != "" {
						t.values = append(t.values, part)
					}
				}
			}
		} else {
			t.values = []string{strings.Trim(w, `"`)}
		}
		out = append(out, t)
	}
	return out
}

// jiraQueryWords splits on spaces outside quotes.
func jiraQueryWords(q string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range q {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case r == ' ' && !quoted:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// jiraSplitTerm reads "field op value" off w, the field a known one.
func jiraSplitTerm(w string) (field, op, value string, ok bool) {
	i := strings.IndexAny(w, ":<>=")
	if i <= 0 {
		return "", "", "", false
	}
	field, ok = jiraQueryFields[w[:i]]
	if !ok {
		return "", "", "", false
	}
	op, rest := w[i:i+1], w[i+1:]
	if (op == ">" || op == "<") && strings.HasPrefix(rest, "=") {
		op, rest = op+"=", rest[1:]
	}
	return field, op, rest, true
}

// match reports whether c has the term (before not).
func (t jiraTerm) match(c jira.Card) bool {
	if t.field == "" {
		q := t.values[0]
		for _, s := range []string{c.Key, c.Summary, c.Assignee, c.ParentKey, c.ParentSummary} {
			if strings.Contains(strings.ToLower(s), q) {
				return true
			}
		}
		return false
	}
	if t.field == "is" {
		for _, v := range t.values {
			if jiraCardIs(c, v) {
				return true
			}
		}
		return false
	}
	var have string
	switch t.field {
	case "status":
		have = c.Status
	case "assignee":
		have = c.Assignee
	case "type":
		have = c.Type
	case "priority":
		have = c.Priority
	case "parent":
		have = c.ParentKey + " " + c.ParentSummary
	case "label":
		have = c.Labels
	case "key":
		have = c.Key
	case "points":
		have = c.Points
	}
	have = strings.ToLower(strings.TrimSpace(have))
	if len(t.values) == 0 {
		return have == ""
	}
	for _, v := range t.values {
		if jiraCompare(t.field, t.op, have, v) {
			return true
		}
	}
	return false
}

// jiraCompare applies op to a card's value have and a query's want.
func jiraCompare(field, op, have, want string) bool {
	switch {
	case op == ":" && field == "label":
		return slices.ContainsFunc(strings.Fields(have), func(l string) bool { return strings.Contains(l, want) })
	case op == ":":
		return strings.Contains(have, want)
	case field == "priority": // a lower rank is a higher priority
		if have == "" {
			return false
		}
		return jiraOrder(op, jiraPriorityRank(want), jiraPriorityRank(have))
	case field == "points":
		h, err1 := strconv.ParseFloat(have, 64)
		w, err2 := strconv.ParseFloat(want, 64)
		return err1 == nil && err2 == nil && jiraOrder(op, h, w)
	}
	return op == "=" && have == want
}

// jiraOrder is a op b.
func jiraOrder[T int | float64](op string, a, b T) bool {
	switch op {
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "<":
		return a < b
	case "<=":
		return a <= b
	}
	return a == b
}

// jiraCardIs answers is:flagged, done, pr and unassigned.
func jiraCardIs(c jira.Card, what string) bool {
	switch what {
	case "flagged":
		return c.Flagged
	case "done":
		return c.Done
	case "pr":
		return c.PR != ""
	case "unassigned":
		return c.Assignee == ""
	}
	return false
}

// applyJiraSearch rebuilds the lanes for the current query, keeping the
// selection when the card still shows.
func (m *Model) applyJiraSearch() {
	keep := m.selectedJiraKey()
	m.buildJiraLanes()
	m.selectJiraKey(keep)
	m.renderJira()
}

// removeSearchTerm drops the query's i-th term (a chip's ×, the builder).
func (m *Model) removeSearchTerm(i int) {
	t := m.jiraTab
	words := jiraQueryWords(t.search.Value())
	if i < 0 || i >= len(words) {
		return
	}
	words = slices.Delete(words, i, i+1)
	if len(words) == 0 {
		m.clearJiraSearch()
		return
	}
	t.search.SetValue(strings.Join(words, " "))
	m.applyJiraSearch()
}

// clearJiraSearch drops the query and closes the box.
func (m *Model) clearJiraSearch() {
	t := m.jiraTab
	t.searching = false
	t.search.SetValue("")
	t.search.Blur()
	m.applyJiraSearch()
}

// handleJiraSearchKey types into the search box: enter keeps the query and
// returns to the board, esc clears it, arrows still move the cursor.
func (m Model) handleJiraSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "esc":
		m.clearJiraSearch()
		return m, nil
	case msg.String() == "enter":
		t.searching = false
		t.search.Blur()
		if t.jiraSearchQuery() == "" {
			m.clearJiraSearch()
		} else {
			m.renderJira()
		}
		return m, nil
	case key.Matches(msg, m.keys.InputUp):
		m.moveJiraCursor(-1)
		return m, nil
	case key.Matches(msg, m.keys.InputDown):
		m.moveJiraCursor(1)
		return m, nil
	}
	before := t.search.Value()
	var cmd tea.Cmd
	t.search, cmd = t.search.Update(msg)
	if t.search.Value() != before {
		m.applyJiraSearch()
	}
	return m, cmd
}
