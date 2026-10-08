package ui

import (
	"maps"
	"strconv"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// The charts' lines: the column they count done from (ui.report_done, for
// the board; Jira's resolution when unset) and, for a visit, a second one
// to set beside it.

// chartLines are the done line (nil: Jira's) and the compared one (nil:
// none).
type chartLines struct{ done, compare *jira.Line }

// by is what the charts say they count done by.
func (ln chartLines) by() string {
	if ln.done == nil {
		return i18n.T("by resolution date")
	}
	return i18n.Tf("done = %s →", ln.done.Name)
}

func (m *Model) chartLines() chartLines {
	t := m.jiraTab
	if t.cfg == nil || t.charts == nil {
		return chartLines{}
	}
	first := m.uiConfig.ReportBackwards == "first"
	return chartLines{
		done:    jira.NewLine(t.cfg.Columns, m.uiConfig.ReportDone[strconv.Itoa(m.jiraBoardID())], first),
		compare: jira.NewLine(t.cfg.Columns, t.charts.compare, first),
	}
}

// openChartLinePicker lists the board's columns to count done from, or,
// for compare, to set a second line at.
func (m *Model) openChartLinePicker(compare bool) {
	t := m.jiraTab
	if t.cfg == nil || len(t.cfg.Columns) == 0 {
		return
	}
	ln := m.chartLines()
	var items []jiraPickerItem
	kind, title := jiraPickChartDone, i18n.T("Count done from")
	if compare {
		kind, title = jiraPickChartCompare, i18n.T("Compare with a line at")
	} else {
		items = append(items, jiraPickerItem{id: "", label: i18n.T("Jira: the resolution date"), current: ln.done == nil})
	}
	for _, c := range t.cfg.Columns {
		if compare && ln.done != nil && ln.done.Name == c.Name {
			continue
		}
		items = append(items, jiraPickerItem{id: c.Name, label: c.Name + " →", current: !compare && ln.done != nil && ln.done.Name == c.Name})
	}
	m.startJiraPicker(kind, title, false)
	m.setJiraPickerItems(items)
}

// applyChartLine sets the done line ("" for Jira's) in ui.report_done, or
// the compared one for this visit, and recounts.
func (m *Model) applyChartLine(compare bool, name string) {
	ch := m.jiraTab.charts
	m.closeJiraPicker()
	if ch == nil {
		return
	}
	if compare {
		ch.compare = name
		return
	}
	board := strconv.Itoa(m.jiraBoardID())
	next := maps.Clone(m.uiConfig.ReportDone)
	if name == "" {
		delete(next, board)
	} else {
		if next == nil {
			next = map[string]string{}
		}
		next[board] = name
	}
	if len(next) == 0 {
		next = nil
	}
	m.uiConfig.ReportDone = next
	if ch.compare == name {
		ch.compare = ""
	}
	if m.configPath == "" {
		m.status = i18n.T("no config file to write to: the done line lasts till you quit")
		return
	}
	var v any
	if next != nil {
		v = next
	}
	if err := config.SetUI(m.configPath, "report_done", v); err != nil {
		m.fail(i18n.Tf("ui.report_done: %s", err.Error()))
	}
}
