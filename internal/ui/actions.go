package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/safeterm"
)

// Your own actions (ui.actions): a command run on the selected issue, or
// on the marked ones from the board, with them as JSON on stdin and
// LANEWAY_KEY (LANEWAY_KEYS for several) in the environment. Each is in
// the palette, and on its key when it has one no built-in takes.

const actionTimeout = time.Minute

// actionIssue is an issue as an action reads it.
type actionIssue struct {
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Assignee string `json:"assignee"`
	Priority string `json:"priority"`
	Points   string `json:"points"`
	URL      string `json:"url"`
}

// actionsFrom keeps the usable actions, warning about the rest: no name
// or command, an unknown where or show, a key a built-in already has.
func actionsFrom(as []config.Action, k *keyMap) ([]config.Action, []string) {
	taken := map[string]string{}
	for name, b := range k.keyNames() {
		for _, kk := range b.Keys() {
			taken[kk] = name
		}
	}
	var out []config.Action
	var warn []string
	for i, a := range as {
		label := fmt.Sprintf("ui.actions[%d]", i)
		if a.Name != "" {
			label = "ui.actions." + a.Name
		}
		switch {
		case a.Name == "" || len(a.Command) == 0:
			warn = append(warn, i18n.Tf("%s: needs a name and a command", label))
			continue
		case !slices.Contains([]string{"", "board", "panel", "both"}, a.Where):
			warn = append(warn, i18n.Tf("%s: where %q is not board, panel or both", label, a.Where))
			continue
		case !slices.Contains([]string{"", "status", "pager"}, a.Show):
			warn = append(warn, i18n.Tf("%s: show %q is not status or pager", label, a.Show))
			continue
		}
		if owner, ok := taken[a.Key]; a.Key != "" && ok {
			warn = append(warn, i18n.Tf("%s: %q is %s already; the action stays in the palette", label, a.Key, owner))
			a.Key = ""
		}
		if a.Key != "" {
			taken[a.Key] = a.Name
		}
		out = append(out, a)
	}
	return out, warn
}

// actionOn is whether a applies where the keys are: the board or the panel.
func actionOn(a config.Action, panel bool) bool {
	return a.Where == "" || a.Where == "both" || (a.Where == "panel") == panel
}

// actionForKey is the action bound to k where the keys are.
func (m *Model) actionForKey(k string, panel bool) (int, bool) {
	i := slices.IndexFunc(m.actions, func(a config.Action) bool { return a.Key == k && actionOn(a, panel) })
	return i, i >= 0
}

// actionIssues are what action i runs on: the panel's issue, else the
// marked cards, else the selected one.
func (m *Model) actionIssues(panel bool) []actionIssue {
	if panel && m.jiraIssue != nil {
		is := m.jiraIssue
		return []actionIssue{{is.Key, is.Summary, is.Status, is.Type, is.Assignee, is.Priority, is.StoryPoints, m.jiraClient.BrowseURL(is.Key)}}
	}
	var out []actionIssue
	add := func(key string) {
		if c, ok := m.cardOf(key); ok {
			out = append(out, actionIssue{c.Key, c.Summary, c.Status, c.Type, c.Assignee, c.Priority, c.Points, m.jiraClient.BrowseURL(c.Key)})
		}
	}
	if keys := m.markedKeys(); len(keys) > 0 && m.quickKey == "" {
		for _, k := range keys {
			add(k)
		}
	} else if c, ok := m.selectedJiraCard(); ok {
		add(c.Key)
	}
	return out
}

type actionDoneMsg struct {
	i   int
	out string
	err error
}

// runAction runs action i on the issues where the keys are.
func (m *Model) runAction(i int, panel bool) tea.Cmd {
	a := m.actions[i]
	if m.demo {
		m.status = i18n.Tf("%s: %s", a.Name, errOffInDemo.Error())
		return nil
	}
	issues := m.actionIssues(panel)
	if len(issues) == 0 {
		m.status = i18n.Tf("%s: no issue selected", a.Name)
		return nil
	}
	keys := make([]string, len(issues))
	for j, is := range issues {
		keys[j] = is.Key
	}
	var in []byte
	if len(issues) == 1 {
		in, _ = json.Marshal(issues[0])
	} else {
		in, _ = json.Marshal(issues)
	}
	m.status = i18n.Tf("running %s on %s…", a.Name, strings.Join(keys, ", "))
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, actionTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, a.Command[0], a.Command[1:]...)
		cmd.Stdin = bytes.NewReader(in)
		cmd.Env = append(os.Environ(), "LANEWAY_KEY="+keys[0], "LANEWAY_KEYS="+strings.Join(keys, " "))
		out, err := cmd.CombinedOutput()
		return actionDoneMsg{i: i, out: strings.TrimRight(string(out), "\n"), err: err}
	}
}

func (m Model) handleActionDone(msg actionDoneMsg) (tea.Model, tea.Cmd) {
	if msg.i >= len(m.actions) {
		return m, nil
	}
	a := m.actions[msg.i]
	lines := strings.Split(safeterm.Text(msg.out), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	switch {
	case msg.err != nil:
		m.fail(i18n.Tf("%s: %s %s", a.Name, msg.err.Error(), last))
	case a.Show == "pager" && msg.out != "":
		m.startJiraPicker(jiraPickActionOutput, a.Name, false)
		items := make([]jiraPickerItem, len(lines))
		for j, l := range lines {
			items[j] = jiraPickerItem{label: l}
		}
		m.setJiraPickerItems(items)
	case last != "":
		m.status = i18n.Tf("%s: %s", a.Name, last)
	default:
		m.status = i18n.Tf("%s done", a.Name)
	}
	if !a.Refresh || msg.err != nil {
		return m, nil
	}
	cmds := []tea.Cmd{m.refreshJiraAfterEdit()}
	if m.refOpen {
		cmds = append(cmds, m.loadCurrentRef())
	}
	return m, tea.Batch(cmds...)
}
