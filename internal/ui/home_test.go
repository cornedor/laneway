package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
)

// TestHome: ~ lists ui.home's widgets in its order against the demo's
// Jira, enter on an issue opens it, on a search runs it as a view.
func TestHome(t *testing.T) {
	url, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: url, Email: "demo@example.com", APIToken: "demo"})
	m.opts.home = []string{"sprint", "work", "timer", "filters"}
	m.timer = workTimer{key: "ABC-2", start: time.Now()}
	m.setJQLList(jqlSavedMeta, []string{"priority = High"})

	out, cmd := m.handleJiraKey(keyMsg(t, "~"))
	m = out.(Model)
	if cmd == nil || !m.jiraPicker.active || m.jiraPicker.kind != jiraPickHome {
		t.Fatal("~ should open home")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	var heads []string
	at := map[string]int{}
	for i, it := range m.jiraPicker.items {
		if !strings.HasPrefix(it.label, " ") {
			heads = append(heads, strings.SplitN(it.label, "  ·  ", 2)[0])
		}
		at[it.id] = i
	}
	if strings.Join(heads, ",") != "Sprint,My work,Timer,Saved searches" {
		t.Errorf("headings %q", heads)
	}
	items := m.jiraPicker.items
	if !strings.Contains(items[0].label, "Sprint 12") || !strings.Contains(items[1].label, "points done") {
		t.Errorf("sprint rows %q, %q", items[0].label, items[1].label)
	}
	if i, ok := at[homeIssue+"ABC-2"]; !ok || !strings.Contains(items[i].label, "ABC-2 0m  Second") {
		t.Errorf("timer row: %v", items)
	}
	if i, ok := at[homeJQL+"priority = High"]; !ok || !strings.HasSuffix(items[i].label, "·  3") {
		t.Errorf("saved search row missing or miscounted: %v", items)
	}

	work := at[homeGo+"work"] + 1
	if !strings.HasPrefix(items[work].id, homeIssue) {
		t.Fatalf("first work row %+v", items[work])
	}
	key := strings.TrimPrefix(items[work].id, homeIssue)
	m.jiraPicker.idx = work
	out, _ = m.handleKey(keyMsg(t, "enter"))
	if m2 := out.(Model); m2.jiraPicker.active || m2.currentRef() == nil || m2.currentRef().jiraKey != key {
		t.Errorf("enter on %s: picker %v, panel %+v", key, m2.jiraPicker.active, m2.currentRef())
	}

	m.jiraPicker.idx = at[homeJQL+"priority = High"]
	out, _ = m.handleKey(keyMsg(t, "enter"))
	if m = out.(Model); m.jiraTab.views[len(m.jiraTab.views)-1].jql != "priority = High" {
		t.Errorf("the search should be a view: %+v", m.jiraTab.views[len(m.jiraTab.views)-1])
	}
}

// TestHomeAtStart: with ui.home set, the first board opens home, once.
func TestHomeAtStart(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	m.opts.home = []string{"timer"}
	msg := jiraBoardMsg{seq: m.jiraTab.seq, project: "ABC", boards: []jira.Board{{ID: 1, Name: "B", Type: "scrum"}}, cfg: &jira.BoardConfig{}}
	out, _ := m.handleJiraBoard(msg)
	if m = out.(Model); !m.jiraPicker.active || m.jiraPicker.kind != jiraPickHome {
		t.Fatal("ui.home should open home on the first board")
	}
	m.closeJiraPicker()
	msg.seq = m.jiraTab.seq
	m.jiraTab.fresh = 0
	if out, _ = m.handleJiraBoard(msg); out.(Model).jiraPicker.active {
		t.Error("home opened again on the next board")
	}
}

// TestHomeInboxArrives: home open at start, before the first inbox sync,
// reads again when it lands, cursor untouched.
func TestHomeInboxArrives(t *testing.T) {
	m := jiraTabModel(t)
	m.opts.home = []string{"inbox"}
	m.openHome()
	d := m.inboxData()
	out, cmd := m.handleInboxSync(inboxSyncMsg{seq: d.seq})
	if m = out.(Model); cmd == nil || m.jiraPicker.kind != jiraPickHome || m.jiraPicker.gen < 1 {
		t.Fatal("the first sync should read home again")
	}
	m.jiraPicker.idx = 1
	if _, cmd = m.handleInboxSync(inboxSyncMsg{seq: m.inboxData().seq}); cmd != nil {
		t.Error("a later sync should leave home as it is")
	}
}
