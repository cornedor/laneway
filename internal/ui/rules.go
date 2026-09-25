package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/rules"
	"github.com/cornedor/laneway/internal/safeterm"
)

// rulesLoggedMsg reports a failed rule action.
type rulesLoggedMsg struct{ err error }

// rulesEventsMsg carries events whose authors were looked up, to fire.
type rulesEventsMsg struct {
	events []rules.Event
	err    error
}

// ruleExecTimeout bounds one exec action.
const ruleExecTimeout = 30 * time.Second

// runRules fires the rules over what changed since this board, view and
// filter last loaded. The first load of each is only remembered: a view
// switch or a filter is not a change. When a rule reads by_me, each
// event's author is looked up first.
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
	events := rules.Diff(prev, cards)
	if len(events) == 0 {
		return nil
	}
	if !m.rules.UsesByMe() {
		return m.fireRules(events)
	}
	ctx, c, points := m.ctx, m.jiraClient, ""
	if t.cfg != nil {
		points = t.cfg.PointsField
	}
	return func() tea.Msg {
		return rulesEventsMsg{events, resolveByMe(ctx, c, events, points)}
	}
}

// resolveByMe sets each event's ByMe from the Jira changelog, leaving it
// nil where the lookup fails.
func resolveByMe(ctx context.Context, c *jira.Client, events []rules.Event, pointsField string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	me, err := c.Myself(ctx)
	if err != nil {
		return err
	}
	fields := map[string]string{rules.New: "", rules.Status: "status", rules.Assignee: "assignee",
		rules.Priority: "priority", rules.Summary: "summary", rules.Points: pointsField}
	var firstFail error
	for i := range events {
		ev := &events[i]
		field := fields[ev.Kind]
		if ev.Kind == rules.Points && field == "" {
			continue
		}
		who, err := c.ChangeAuthor(ctx, ev.Card.Key, field)
		if err != nil {
			firstFail = firstErr(firstFail, err)
			continue
		}
		byMe := who == me.AccountID
		ev.ByMe = &byMe
	}
	return firstFail
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
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, ev := range events {
		for _, f := range m.rules.Fire(ev) {
			switch f.Action {
			case "log":
				lines = append(lines, fmt.Sprintf("%s %s: %s\n", now, orUnnamed(f.Rule), safeterm.Line(f.Text)))
			case "notify":
				cmds = append(cmds, tea.Raw(notifySeq(f.Title, f.Text)))
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
		cmds = append(cmds, func() tea.Msg {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				_, err = f.WriteString(strings.Join(lines, ""))
				err = firstErr(err, f.Close())
			}
			return rulesLoggedMsg{err}
		})
	}
	if marked {
		t.rows = nil
		m.renderJira()
	}
	return tea.Batch(cmds...)
}

// notifySeq is a desktop notification by OSC 777, which kitty, Ghostty,
// WezTerm and foot show; other terminals ignore it.
func notifySeq(title, body string) string {
	clean := func(s string) string { return strings.ReplaceAll(safeterm.Line(s), ";", ",") }
	return "\x1b]777;notify;" + clean(title) + ";" + clean(body) + "\x1b\\"
}

// ruleExec runs an exec action's argv with the issue as JSON on stdin and
// as LANEWAY_* variables (LANEWAY_KEY, LANEWAY_OLD_STATUS, …).
func (m *Model) ruleExec(f rules.Firing) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, ruleExecTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, f.Argv[0], f.Argv[1:]...)
		in, _ := json.Marshal(f.Vars)
		cmd.Stdin = bytes.NewReader(in)
		cmd.Env = os.Environ()
		for k, v := range f.Vars {
			cmd.Env = append(cmd.Env, "LANEWAY_"+envName(k)+"="+v)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return rulesLoggedMsg{fmt.Errorf("%s: %s: %v %s", orUnnamed(f.Rule), f.Argv[0], err, bytes.TrimSpace(out))}
		}
		return nil
	}
}

// envName turns OldStatus into OLD_STATUS.
func envName(k string) string {
	var b strings.Builder
	for i, r := range k {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

func orUnnamed(name string) string {
	if name == "" {
		return "rule"
	}
	return name
}
