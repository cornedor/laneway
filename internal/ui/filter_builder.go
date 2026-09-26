package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// The filter builder (F): a field, how to compare and a value from the
// loaded cards (with how many have it), side by side; the term lands in the
// / query, joining a term on the same field as one more value.

// filterFields are the builder's fields: the query name and a label.
var filterFields = []struct{ name, label string }{
	{"status", "Status"}, {"assignee", "Assignee"}, {"type", "Type"}, {"prio", "Priority"},
	{"points", "Story points"}, {"label", "Label"}, {"epic", "Epic"}, {"pr", "Pull request"}, {"deploy", "Deployed to"},
	{"component", "Component"}, {"reporter", "Reporter"},
	{"is", "Is: mine, overdue, flagged…"},
}

// filterBuilder is the open builder: three columns side by side (field,
// compare, value), each narrowed by typing while it has the cursor. The
// term being built shows live; enter adds it to the / query and the builder
// stays open for the next.
type filterBuilder struct {
	col    int    // 0 field, 1 compare, 2 value
	idx    [3]int // cursor per column, into its narrowed rows
	filter textinput.Model
}

func (m *Model) openFilterBuilder() {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "type to narrow"
	ti.SetWidth(30)
	ti.Focus()
	m.filterBuilder = &filterBuilder{filter: ti}
}

// filterOps are the ways to compare field.
func filterOps(field string) []jiraPickerItem {
	switch field {
	case "is":
		return []jiraPickerItem{{id: ":", label: "is"}}
	case "prio", "points":
		return []jiraPickerItem{{id: ">=", label: "at least"}, {id: "<=", label: "at most"}, {id: "=", label: "exactly"}}
	}
	return []jiraPickerItem{{id: ":", label: "is"}, {id: "-:", label: "is not"}, {id: "empty", label: "is empty"}}
}

// filterValueItems are field's values on the loaded cards with their
// counts, most common first (priorities by rank).
func (m *Model) filterValueItems(field string) []jiraPickerItem {
	counts, labels := filterValues(m.jiraTab.cards, field, m.jiraQueryEnv())
	values := slices.Collect(maps.Keys(counts))
	slices.SortFunc(values, func(x, y string) int {
		if field == "prio" {
			return cmp.Compare(jiraPriorityRank(x), jiraPriorityRank(y))
		}
		return cmp.Or(cmp.Compare(counts[y], counts[x]), cmp.Compare(strings.ToLower(x), strings.ToLower(y)))
	})
	items := make([]jiraPickerItem, len(values))
	for i, v := range values {
		items[i] = jiraPickerItem{id: v, label: fmt.Sprintf("%s · %d", labels[v], counts[v])}
	}
	return items
}

// builderRows are column col's rows, narrowed by the filter when it has the
// cursor.
func (m *Model) builderRows(col int) []jiraPickerItem {
	b := m.filterBuilder
	var rows []jiraPickerItem
	switch col {
	case 0:
		for _, f := range filterFields {
			rows = append(rows, jiraPickerItem{id: f.name, label: f.label})
		}
	case 1:
		rows = filterOps(m.builderPick(0).id)
	case 2:
		if m.builderPick(1).id != "empty" {
			rows = m.filterValueItems(m.builderPick(0).id)
		}
	}
	if col != b.col {
		return rows
	}
	terms := strings.Fields(strings.ToLower(b.filter.Value()))
	return slices.DeleteFunc(rows, func(it jiraPickerItem) bool {
		label := strings.ToLower(it.label)
		return slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(label, t) })
	})
}

// builderPick is column col's row under its cursor, zero when none.
func (m *Model) builderPick(col int) jiraPickerItem {
	rows := m.builderRows(col)
	if i := m.filterBuilder.idx[col]; i < len(rows) {
		return rows[i]
	}
	return jiraPickerItem{}
}

// builderTerm is the term the columns spell, "" while it lacks a part.
func (m *Model) builderTerm() string {
	field, op, v := m.builderPick(0).id, m.builderPick(1).id, m.builderPick(2).id
	switch {
	case field == "" || op == "":
		return ""
	case op == "empty":
		return field + ":"
	case v == "":
		return ""
	}
	if strings.ContainsAny(v, " ,") {
		v = `"` + v + `"`
	}
	if op == "-:" {
		return "-" + field + ":" + v
	}
	return field + op + v
}

func (m Model) handleFilterBuilderKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := m.filterBuilder
	move := func(col int) {
		b.col = min(max(col, 0), 2)
		b.filter.SetValue("")
	}
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "esc":
		m.filterBuilder = nil
		return m, nil
	case msg.String() == "enter":
		if b.col == 0 || b.col == 1 && m.builderPick(1).id != "empty" {
			move(b.col + 1)
			return m, nil
		}
		if term := m.builderTerm(); term != "" {
			m.addFilterTerm(term)
			b.idx[2] = 0
			b.filter.SetValue("")
		}
		return m, nil
	case msg.String() == "tab", msg.String() == "right":
		move(b.col + 1)
		return m, nil
	case msg.String() == "shift+tab", msg.String() == "left":
		move(b.col - 1)
		return m, nil
	case msg.String() == "ctrl+x":
		m.removeSearchTerm(len(jiraQueryWords(m.jiraTab.search.Value())) - 1)
		return m, nil
	case key.Matches(msg, m.keys.InputUp):
		b.idx[b.col] = max(b.idx[b.col]-1, 0)
	case key.Matches(msg, m.keys.InputDown):
		b.idx[b.col] = min(b.idx[b.col]+1, max(len(m.builderRows(b.col))-1, 0))
	default:
		before := b.filter.Value()
		var cmd tea.Cmd
		b.filter, cmd = b.filter.Update(msg)
		if b.filter.Value() != before {
			b.idx[b.col] = 0
		}
		return m, cmd
	}
	// A new field or compare starts the columns after it afresh.
	for c := b.col + 1; c < 3; c++ {
		b.idx[c] = 0
	}
	return m, nil
}

// builderWidths are the columns' widths, a gap of 2 between them.
var builderWidths = [3]int{24, 10, 30}

// window is column c's first row shown and how many show in height, of n.
func (b *filterBuilder) window(c, n, height int) (top, visible int) {
	visible = max(min(height-14, 12), 3)
	return min(max(b.idx[c]-visible+1, 0), max(n-visible, 0)), visible
}

func (m *Model) renderFilterBuilder(height int) string {
	b := m.filterBuilder
	widths := builderWidths
	titles := [3]string{"Field", "Compare", "Value"}
	var cols []string
	for c := range 3 {
		rows := m.builderRows(c)
		top, visible := b.window(c, len(rows), height)
		head := titleStyle.Render(titles[c])
		if c == b.col {
			head = jiraViewActive.Render(titles[c])
		}
		lines := []string{head}
		for i := top; i < min(top+visible, len(rows)); i++ {
			label := truncate(rows[i].label, widths[c])
			label += strings.Repeat(" ", widths[c]-lipgloss.Width(label))
			switch {
			case i == b.idx[c] && c == b.col:
				label = selectedRow.Render(label)
			case i == b.idx[c]:
				label = diffTreeSelStyle.Render(label)
			case c > b.col:
				label = jiraDimStyle.Render(label)
			}
			lines = append(lines, label)
		}
		if len(rows) == 0 && c == 2 && m.builderPick(1).id == "empty" {
			lines = append(lines, jiraDimStyle.Render("no value needed"))
		}
		cols = append(cols, lipgloss.NewStyle().Width(widths[c]).Render(strings.Join(lines, "\n")))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols[0], "  ", cols[1], "  ", cols[2])
	term := m.builderTerm()
	if term == "" {
		term = jiraDimStyle.Render("pick a value")
	} else {
		term = jiraKeyStyle.Render(term)
	}
	query := jiraDimStyle.Render("/" + m.jiraTab.search.Value())
	hint := lipgloss.NewStyle().Foreground(dimColor).Italic(true).Render("↑↓ pick · ←→ tab column · ↵ add · ctrl+x drop last · esc close")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(focusedColor).Padding(1, 3).
		Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("Filter"), query, "", b.filter.View(), "", body, "", "adds  "+term, "", hint))
}

// filterValues counts each value of field over cards; labels is what a row
// shows for it (an epic's key with its summary).
func filterValues(cards []jira.Card, field string, env jiraQueryEnv) (counts map[string]int, labels map[string]string) {
	counts, labels = map[string]int{}, map[string]string{}
	add := func(v, label string) {
		if v != "" {
			counts[v]++
			labels[v] = label
		}
	}
	for _, c := range cards {
		switch field {
		case "status":
			add(c.Status, c.Status)
		case "assignee":
			add(c.Assignee, c.Assignee)
		case "type":
			add(c.Type, c.Type)
		case "prio":
			add(c.Priority, c.Priority)
		case "points":
			add(c.Points, c.Points)
		case "label":
			for _, l := range strings.Fields(c.Labels) {
				add(l, l)
			}
		case "component":
			for _, n := range strings.Split(c.Components, jira.ExtraSep) {
				add(n, n)
			}
		case "reporter":
			add(c.Reporter, c.Reporter)
		case "pr":
			add(strings.ToLower(c.PR), strings.ToLower(c.PR))
		case "deploy":
			add(c.Deploy, c.Deploy)
		case "epic":
			add(c.ParentKey, strings.TrimSpace(c.ParentKey+" "+c.ParentSummary))
		case "is":
			for _, v := range []string{"mine", "overdue", "flagged", "done", "pr", "unassigned"} {
				if jiraCardIs(c, v, env) {
					add(v, v)
				}
			}
		}
	}
	return counts, labels
}

// addFilterTerm puts term in the / query: a ":" term on the same field
// takes its value as one more, a comparison replaces its like.
func (m *Model) addFilterTerm(term string) {
	prefix, v := term, ""
	if i := strings.IndexAny(term, ":<>="); i > 0 {
		j := i + 1
		for j < len(term) && term[j] == '=' {
			j++
		}
		prefix, v = term[:j], term[j:]
	}
	t := m.jiraTab
	words := jiraQueryWords(t.search.Value())
	merged := false
	for i, w := range words {
		if !strings.HasPrefix(strings.ToLower(w), prefix) {
			continue
		}
		switch {
		case !strings.HasSuffix(prefix, ":"):
			words[i] = prefix + v
		case v != "" && len(w) > len(prefix):
			words[i] = w + "," + v
		default:
			words[i] = prefix + v
		}
		merged = true
		break
	}
	if !merged {
		words = append(words, prefix+v)
	}
	t.search.SetValue(strings.Join(words, " "))
	m.applyJiraSearch()
	m.status = "/" + t.search.Value() + " · esc clears"
}

func filterLabel(field string) string {
	for _, f := range filterFields {
		if f.name == field {
			return f.label
		}
	}
	return field
}
