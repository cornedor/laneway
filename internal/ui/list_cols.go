package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/i18n"
)

// listColumnIDs are the list view's columns in their default order.
var listColumnIDs = []string{"key", "type", "priority", "status", "points", "summary", "assignee", "marks"}

// listColumnsFrom is ui.list_columns as an order of every column: those
// named first, the rest after in the default order.
func listColumnsFrom(names []string) (order, warn []string) {
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		switch {
		case !slices.Contains(listColumnIDs, n):
			warn = append(warn, i18n.Tf("ui.list_columns: unknown column %q", n))
		case !slices.Contains(order, n):
			order = append(order, n)
		}
	}
	for _, id := range listColumnIDs {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	return order, warn
}

// listColumnOrder is the list's column order, shown or not.
func (m *Model) listColumnOrder() []string {
	if len(m.opts.listCols) == len(listColumnIDs) {
		return m.opts.listCols
	}
	return listColumnIDs
}

// listColumn is a shown column of the list: its width and the gap after it.
type listColumn struct {
	id     string
	w, gap int
}

// jiraListLayout is the list's shown columns in order, for rows of cols;
// the summary takes what the others leave.
func (m *Model) jiraListLayout(cols listCols) []listColumn {
	f := m.opts.fields
	var out []listColumn
	for _, id := range m.listColumnOrder() {
		c := listColumn{id: id, gap: 2}
		switch id {
		case "key":
			c.w = cols.key
		case "type":
			c.w, c.gap = visualWidth(jiraTypeIcon("task", "")), 1
		case "priority":
			c.w, c.gap = 1, 1
		case "status":
			c.w = cols.status
		case "points":
			c.w = 4
		case "summary":
			c.w = -1
		case "assignee":
			c.w = cols.who
		case "marks":
			c.w = cols.marks
		}
		if id == "type" && !f.typ || id == "priority" && !f.priority || id == "status" && !f.status || id == "points" && !f.points || c.w == 0 {
			continue
		}
		out = append(out, c)
	}
	out[len(out)-1].gap = 0
	fixed := 2 // the row's mark
	for _, c := range out {
		fixed += c.gap + max(c.w, 0)
	}
	for i := range out {
		if out[i].w < 0 {
			out[i].w = max(cols.width-1-fixed, 12)
		}
	}
	return out
}

// listColumnName is a column's name in messages.
func listColumnName(id string) string {
	return strings.ToUpper(id[:1]) + id[1:]
}

// headCell is a cell of the list's header: columns x0 to x1 of the body,
// sorting by by (headAt) or moving column col (headCols).
type headCell struct {
	x0, x1 int
	by     jiraSort
	col    string
}

// jiraListHeader is the list's column header over rows laid out as
// jiraListRow lays them out with cols, its sortable cells and its columns.
// A sort with no column of its own (updated, due, created) shows at the
// summary's end.
func (m *Model) jiraListHeader(cols listCols) (string, []headCell, []headCell) {
	t := m.jiraTab
	f := m.opts.fields
	cur := viewSort{by: t.sort, desc: t.desc}
	var b strings.Builder
	var at, spans []headCell
	x := 2
	b.WriteString("  ")
	// cell writes label in w columns, right-aligned when right;
	// jiraSortRank for a label that doesn't sort.
	cell := func(label string, w int, by jiraSort, right bool) {
		st := jiraDimStyle
		if by != jiraSortRank && cur.by == by {
			st = jiraViewActive
			if visualWidth(label)+1 > w {
				label = ansi.Truncate(label, max(w-1, 0), "")
			}
			label += cur.arrow()
		}
		label = ansi.Truncate(label, w, "…")
		pad := strings.Repeat(" ", max(w-visualWidth(label), 0))
		if right {
			b.WriteString(pad + st.Render(label))
		} else {
			b.WriteString(st.Render(label) + pad)
		}
		if by != jiraSortRank {
			at = append(at, headCell{x0: x, x1: x + w, by: by})
		}
		x += w
	}
	shown := map[jiraSort]bool{jiraSortRank: true, jiraSortKey: true, jiraSortPriority: f.priority, jiraSortStatus: f.status,
		jiraSortPoints: f.points, jiraSortEpic: f.parent, jiraSortAssignee: cols.who > 0}
	for _, c := range m.jiraListLayout(cols) {
		start := x
		switch c.id {
		case "key":
			cell(i18n.T("Key"), c.w, jiraSortKey, false)
		case "priority":
			cell(i18n.T("P"), c.w, jiraSortPriority, false)
		case "status":
			cell(i18n.T("Status"), c.w, jiraSortStatus, false)
		case "points":
			cell(i18n.T("Pts"), c.w, jiraSortPoints, true)
		case "assignee":
			cell(i18n.T("Assignee"), c.w, jiraSortAssignee, false)
		case "summary":
			end := x + c.w
			epic, sum := i18n.T("Epic"), i18n.T("Summary")
			cell(sum, min(len(sum), c.w), jiraSortRank, false)
			if f.parent && x+3+len(epic)+1 <= end {
				b.WriteString(jiraDimStyle.Render(" · "))
				x += 3
				cell(epic, len(epic)+1, jiraSortEpic, false)
			}
			if by := cur.by.String(); !shown[cur.by] && x+len(by)+2 <= end {
				w := len(by) + 1
				b.WriteString(strings.Repeat(" ", end-w-x))
				x = end - w
				cell(listColumnName(by), w, cur.by, true)
			}
			b.WriteString(strings.Repeat(" ", max(end-x, 0)))
			x = end
		default:
			cell("", c.w, jiraSortRank, false)
		}
		spans = append(spans, headCell{x0: start, x1: x, col: c.id})
		b.WriteString(strings.Repeat(" ", c.gap))
		x += c.gap
	}
	return ansi.Truncate(b.String(), max(cols.width-1, 0), ""), at, spans
}

// headDrag is a press on the list's header: a click sorts by its cell, a
// drag moves its column before or after another.
type headDrag struct {
	col    string
	sortBy int // the pressed cell's jiraSort + 1, 0 for none
	x      int
	active bool
	to     string
	after  bool
}

// dragJiraHead follows a header cell dragged to x.
func (m Model) dragJiraHead(x int) (tea.Model, tea.Cmd) {
	d := &m.jiraTab.headDrag
	if !d.active && x-d.x < 2 && d.x-x < 2 {
		return m, nil
	}
	d.active = true
	spans := m.jiraTab.headCols
	if len(spans) == 0 {
		return m, nil
	}
	s := spans[len(spans)-1]
	for _, c := range spans {
		if x-1 < c.x1 {
			s = c
			break
		}
	}
	d.to, d.after = s.col, x-1 >= (s.x0+s.x1)/2
	m.status = i18n.Tf("drop to move %s here", listColumnName(d.col))
	if d.to == d.col {
		m.status = i18n.Tf("drag %s onto another column to move it", listColumnName(d.col))
	}
	return m, nil
}

// dropJiraHead ends a press on the header: sorts on a click, moves the
// column on a drag.
func (m Model) dropJiraHead() (tea.Model, tea.Cmd) {
	t := m.jiraTab
	d := t.headDrag
	t.headDrag = headDrag{}
	switch {
	case !d.active && d.sortBy > 0:
		m.sortJiraList(viewSort{by: t.sort, desc: t.desc}.next(jiraSort(d.sortBy - 1)))
	case d.active && d.to != "" && d.to != d.col:
		m.moveListColumn(d.col, d.to, d.after)
	default:
		m.status = ""
	}
	return m, nil
}

// moveListColumn moves column id before to, or after it, and keeps the
// order in ui.list_columns.
func (m *Model) moveListColumn(id, to string, after bool) {
	order := slices.DeleteFunc(slices.Clone(m.listColumnOrder()), func(c string) bool { return c == id })
	i := slices.Index(order, to)
	if after {
		i++
	}
	order = slices.Insert(order, i, id)
	m.opts.listCols = order
	m.jiraTab.rows = nil
	m.renderJira()
	m.status = i18n.Tf("%s before %s", listColumnName(id), listColumnName(to))
	if after {
		m.status = i18n.Tf("%s after %s", listColumnName(id), listColumnName(to))
	}
	if m.configPath == "" {
		m.status += i18n.T("; no config file to write to: it lasts till you quit")
		return
	}
	if err := config.SetUI(m.configPath, "list_columns", order); err != nil {
		m.fail(i18n.Tf("ui.list_columns: %s", err.Error()))
	}
}
