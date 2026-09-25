package ui

import (
	"context"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
)

// rulesLoggedMsg reports a failed rule action.
type rulesLoggedMsg struct{ err error }

// rulesEventsMsg carries events whose authors were looked up, to fire.
type rulesEventsMsg struct {
	events []rules.Event
	err    error
}

// runRules fires the rules over what changed since this board, view and
// filter last loaded. The first load of each is only remembered: a view
// switch or a filter is not a change.
func (m *Model) runRules(cards []jira.Card) tea.Cmd {
	t := m.jiraTab
	if m.rules == nil || m.rules.Len() == 0 {
		return nil
	}
	v, _ := m.jiraCurrentView()
	key := strconv.Itoa(m.jiraBoardID()) + ":" + v.name + ":" + jiraFilterJQL(t.assignee, t.quick, t.quickOn)
	points := ""
	if t.cfg != nil {
		points = t.cfg.PointsField
	}
	return m.diffRules(key, "", cards, points)
}

// diffRules fires the rules over what changed in cards since key was last
// seen; the first sight is only remembered. When a rule reads by_me, each
// event's author is looked up first.
func (m *Model) diffRules(key, watch string, cards []jira.Card, pointsField string) tea.Cmd {
	t := m.jiraTab
	if t.rulesSeen == nil {
		t.rulesSeen = map[string][]jira.Card{}
	}
	prev, ok := t.rulesSeen[key]
	t.rulesSeen[key] = slices.Clone(cards) // a lane move edits t.cards in place
	if !ok {
		return nil
	}
	events := rules.Diff(prev, cards)
	for i := range events {
		events[i].Watch = watch
	}
	if len(events) == 0 {
		return nil
	}
	if !m.rules.UsesByMe() {
		return m.fireRules(events)
	}
	ctx, c := m.ctx, m.jiraClient
	return func() tea.Msg {
		return rulesEventsMsg{events, rules.ResolveByMe(ctx, c, events, pointsField)}
	}
}

// ruleWatchMsg is the tick to poll a rule watch.
type ruleWatchMsg struct{ jql string }

// ruleWatchedMsg carries what a rule watch's search found.
type ruleWatchedMsg struct {
	jql   string
	cards []jira.Card
	err   error
}

// startRuleWatches runs every rule watch's first search, which is only
// remembered.
func (m *Model) startRuleWatches() tea.Cmd {
	if m.rules == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, w := range m.rules.Watches() {
		cmds = append(cmds, m.pollRuleWatch(w.JQL))
	}
	return tea.Batch(cmds...)
}

func (m *Model) pollRuleWatch(jql string) tea.Cmd {
	ctx, c := m.ctx, m.jiraClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cards, err := c.SearchCards(ctx, jql)
		return ruleWatchedMsg{jql, cards, err}
	}
}

// handleRuleWatched fires the rules over what the watch's search changed
// and arms its next poll; a failed search keeps the last cards.
func (m Model) handleRuleWatched(msg ruleWatchedMsg) (tea.Model, tea.Cmd) {
	every := rules.DefaultEvery
	for _, w := range m.rules.Watches() {
		if w.JQL == msg.jql {
			every = w.Every
		}
	}
	next := tea.Tick(every, func(time.Time) tea.Msg { return ruleWatchMsg{msg.jql} })
	if msg.err != nil {
		m.status = "rules watch: " + msg.err.Error()
		return m, next
	}
	return m, tea.Batch(m.diffRules("watch:"+msg.jql, msg.jql, msg.cards, ""), next)
}

func (m Model) handleRulesEvents(msg rulesEventsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "rules by_me: " + msg.err.Error()
	}
	return m, m.fireRules(msg.events)
}

// fireRules runs the actions of every rule each event fires.
func (m *Model) fireRules(events []rules.Event) tea.Cmd {
	t := m.jiraTab
	var lines []string
	var cmds []tea.Cmd
	marked := false
	now := time.Now()
	for _, ev := range events {
		for _, f := range m.rules.Fire(ev) {
			switch f.Action {
			case "log":
				lines = append(lines, rules.LogLine(now, f))
			case "notify":
				cmds = append(cmds, tea.Raw(rules.NotifySeq(f.Title, f.Text)))
			case "exec":
				cmds = append(cmds, m.ruleExec(f))
			case "highlight":
				if t.highlights == nil {
					t.highlights = map[string]string{}
				}
				t.highlights[f.Vars["Key"]] = f.Color
				marked = true
			}
		}
	}
	if len(lines) > 0 && m.rulesLog != "" {
		path := m.rulesLog
		cmds = append(cmds, func() tea.Msg { return rulesLoggedMsg{rules.AppendLog(path, lines)} })
	}
	if marked {
		t.rows = nil
		m.renderJira()
	}
	return tea.Batch(cmds...)
}

// ruleExec runs an exec action.
func (m *Model) ruleExec(f rules.Firing) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		if err := rules.Exec(ctx, f); err != nil {
			return rulesLoggedMsg{err}
		}
		return nil
	}
}
