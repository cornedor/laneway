package ui

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// rulesLoggedMsg reports a failed write to the rules log.
type rulesLoggedMsg struct{ err error }

// runRules fires the rules over what changed since this board, view and
// filter last loaded. The first load of each is only remembered: a view
// switch or a filter is not a change. Your own edits count as changes.
func (m *Model) runRules(cards []jira.Card) tea.Cmd {
	t := m.jiraTab
	if m.rules == nil || m.rules.Len() == 0 {
		return nil
	}
	v, _ := m.jiraCurrentView()
	key := strconv.Itoa(m.jiraBoardID()) + ":" + v.name + ":" + jiraFilterJQL(t.assignee, t.quick, t.quickOn)
	if t.rulesSeen == nil {
		t.rulesSeen = map[string][]jira.Card{}
	}
	prev, ok := t.rulesSeen[key]
	t.rulesSeen[key] = slices.Clone(cards) // a lane move edits t.cards in place
	if !ok {
		return nil
	}
	var lines []string
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, ev := range rules.Diff(prev, cards) {
		for _, f := range m.rules.Fire(ev) {
			lines = append(lines, fmt.Sprintf("%s %s: %s\n", now, orUnnamed(f.Rule), f.Text))
		}
	}
	if len(lines) == 0 || m.rulesLog == "" {
		return nil
	}
	path := m.rulesLog
	return func() tea.Msg {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = f.WriteString(strings.Join(lines, ""))
			err = firstErr(err, f.Close())
		}
		return rulesLoggedMsg{err}
	}
}

func orUnnamed(name string) string {
	if name == "" {
		return "rule"
	}
	return name
}
