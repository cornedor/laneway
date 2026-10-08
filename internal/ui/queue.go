package ui

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/offline"
)

// Offline writes: a change that couldn't reach Jira waits in the state
// file (⇡3 in the header) and is sent again every queueEvery, oldest
// first. One whose issue changed in Jira since stops the replay rather
// than overwrite it: the palette's queue row sends it anyway or drops it.

const queueEvery = offline.Every

var (
	readQueue  = offline.Read
	writeQueue = offline.Write
	queueTo    = offline.To
)

type queueTickMsg struct{}

type queueReplayedMsg struct {
	sent     int
	failed   []string // writes Jira refused, dropped
	conflict string   // the issue that changed since, where it stopped
	left     int
	err      error
}

func queueTick() tea.Cmd {
	return tea.Tick(queueEvery, func(time.Time) tea.Msg { return queueTickMsg{} })
}

func (m Model) handleQueueTick() (tea.Model, tea.Cmd) {
	if m.queued = len(readQueue(m.store)); m.queued == 0 {
		return m, queueTick()
	}
	return m, tea.Batch(m.replayQueue(false), queueTick())
}

// replayQueue sends the queued writes, oldest first; force skips the check
// that the issue hasn't changed since.
func (m *Model) replayQueue(force bool) tea.Cmd {
	c, ctx, st := m.jiraClient, m.ctx, m.store
	return func() tea.Msg {
		r := offline.Replay(ctx, c, st, force)
		return queueReplayedMsg{sent: r.Sent, failed: r.Failed, conflict: r.Conflict, left: r.Left, err: r.Err}
	}
}

func (m Model) handleQueueReplayed(msg queueReplayedMsg) (tea.Model, tea.Cmd) {
	m.queued = msg.left
	for _, f := range msg.failed {
		m.fail(i18n.Tf("queued write refused, dropped: %s", f))
	}
	switch {
	case msg.conflict != "":
		m.status = i18n.Tf("%s changed in Jira since you queued a write · %s → queue sends it anyway or drops it", msg.conflict, helpKey(m.keys.Palette))
	case msg.sent > 0:
		m.status = i18n.Tn(msg.sent, "back online: sent %d queued write", "back online: sent %d queued writes", msg.sent)
		if msg.left > 0 {
			m.status += i18n.Tf(", %d left", msg.left)
		}
		return m, m.refreshJiraAfterEdit()
	}
	return m, nil
}

// queueBadge is the header's "⇡3", "" with nothing queued.
func (m *Model) queueBadge() string {
	if m.queued == 0 {
		return ""
	}
	return "⇡" + strconv.Itoa(m.queued)
}

// openQueue lists the queued writes: send them now, or drop one (enter
// twice).
func (m *Model) openQueue() {
	m.startJiraPicker(jiraPickQueue, i18n.T("Offline writes"), false)
	items := []jiraPickerItem{{id: "send", label: i18n.T("Send them now (also over changes made in Jira since)")}}
	for i, w := range readQueue(m.store) {
		items = append(items, jiraPickerItem{id: strconv.Itoa(i), label: i18n.Tf("%s  %s %s  · %s ago · enter twice drops it", w.What, w.Method, w.Path, age(w.At))})
	}
	m.setJiraPickerItems(items)
}

func (m Model) applyQueuePick(it jiraPickerItem) (tea.Model, tea.Cmd) {
	if it.id == "send" {
		m.closeJiraPicker()
		m.status = i18n.T("sending the queued writes…")
		return m, m.replayQueue(true)
	}
	if m.jiraPicker.pendingDelete != it.id {
		m.jiraPicker.pendingDelete = it.id
		m.status = i18n.T("enter again drops this write")
		return m, nil
	}
	m.closeJiraPicker()
	i, _ := strconv.Atoi(it.id)
	m.queued = offline.Drop(m.store, i)
	m.status = i18n.T("dropped the write")
	return m, nil
}
