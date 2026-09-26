package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// The filter builder (F): pick a field, how to compare, then a value from
// the loaded cards (with how many have it); the term lands in the / query,
// joining a term on the same field as one more value. The query's terms
// head the first step, where picking one removes it.

// filterFields are the builder's fields: the query name and a label.
var filterFields = []struct{ name, label string }{
	{"status", "Status"}, {"assignee", "Assignee"}, {"type", "Type"}, {"prio", "Priority"},
	{"points", "Story points"}, {"label", "Label"}, {"epic", "Epic"}, {"pr", "Pull request"}, {"deploy", "Deployed to"},
	{"is", "Mine, overdue, flagged, done, PR, unassigned"},
}

// filterBuild is the builder's choices so far.
type filterBuild struct {
	field, op string // op is a query operator; "-:" is "is not"
}

func (m *Model) openFilterBuilder() {
	m.filterBuild = filterBuild{}
	m.startJiraPicker(jiraPickFilterField, "Filter by", false)
	var items []jiraPickerItem
	for i, w := range jiraQueryWords(m.jiraTab.search.Value()) {
		items = append(items, jiraPickerItem{id: "-" + strconv.Itoa(i), label: "× " + w})
	}
	for _, f := range filterFields {
		items = append(items, jiraPickerItem{id: f.name, label: f.label})
	}
	m.setJiraPickerItems(items)
}

// pickFilterField asks how to compare the field; is: goes straight to its
// values.
func (m *Model) pickFilterField(field string) {
	m.filterBuild.field = field
	if field == "is" {
		m.pickFilterOp(":")
		return
	}
	var items []jiraPickerItem
	switch field {
	case "prio", "points":
		items = []jiraPickerItem{{id: ">=", label: "at least"}, {id: "<=", label: "at most"}, {id: "=", label: "exactly"}}
	default:
		items = []jiraPickerItem{{id: ":", label: "is"}, {id: "-:", label: "is not"}, {id: "empty", label: "is empty"}}
	}
	m.startJiraPicker(jiraPickFilterOp, filterLabel(field), false)
	m.setJiraPickerItems(items)
}

// pickFilterOp lists the field's values on the loaded cards, most common
// first (priorities by rank); "is empty" needs none.
func (m *Model) pickFilterOp(op string) {
	b := &m.filterBuild
	if op == "empty" {
		m.addFilterTerm(b.field+":", "")
		return
	}
	b.op = op
	counts, labels := filterValues(m.jiraTab.cards, b.field, m.jiraQueryEnv())
	values := slices.Collect(maps.Keys(counts))
	slices.SortFunc(values, func(x, y string) int {
		if b.field == "prio" {
			return cmp.Compare(jiraPriorityRank(x), jiraPriorityRank(y))
		}
		return cmp.Or(cmp.Compare(counts[y], counts[x]), cmp.Compare(strings.ToLower(x), strings.ToLower(y)))
	})
	items := make([]jiraPickerItem, len(values))
	for i, v := range values {
		items[i] = jiraPickerItem{id: v, label: fmt.Sprintf("%s · %d", labels[v], counts[v])}
	}
	m.startJiraPicker(jiraPickFilterValue, filterLabel(b.field), len(items) > 9)
	m.setJiraPickerItems(items)
	if len(items) == 0 {
		m.jiraPicker.err = fmt.Errorf("no card has one")
	}
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

func (m *Model) pickFilterValue(v string) {
	b := m.filterBuild
	prefix := b.field + b.op
	if b.op == "-:" {
		prefix = "-" + b.field + ":"
	}
	if strings.ContainsAny(v, " ,") {
		v = `"` + v + `"`
	}
	m.addFilterTerm(prefix, v)
}

// addFilterTerm puts prefix+v in the / query: a ":" term with the same
// prefix takes v as one more value, a comparison replaces its like.
func (m *Model) addFilterTerm(prefix, v string) {
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

// applyFilterPick moves the builder on from a picked row.
func (m Model) applyFilterPick(kind jiraPickerKind, id string) (tea.Model, tea.Cmd) {
	m.closeJiraPicker()
	switch kind {
	case jiraPickFilterField:
		if n, ok := strings.CutPrefix(id, "-"); ok {
			i, _ := strconv.Atoi(n)
			m.removeSearchTerm(i)
			break
		}
		m.pickFilterField(id)
	case jiraPickFilterOp:
		m.pickFilterOp(id)
	case jiraPickFilterValue:
		m.pickFilterValue(id)
	}
	return m, nil
}
