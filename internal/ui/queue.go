package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// Offline writes: a change that couldn't reach Jira waits in the state
// file (⇡3 in the header) and is sent again every queueEvery, oldest
// first. One whose issue changed in Jira since stops the replay rather
// than overwrite it: the palette's queue row sends it anyway or drops it.

const (
	queueMeta  = jiraMetaPrefix + "queue"
	queueEvery = 30 * time.Second
)

// queueMu guards the stored queue: the client adds to it from commands.
var queueMu sync.Mutex

func readQueue(st *store.Store) []jira.PendingWrite {
	if st == nil {
		return nil
	}
	v, _, _ := st.GetMeta(queueMeta)
	var ws []jira.PendingWrite
	_ = json.Unmarshal([]byte(v), &ws)
	return ws
}

func writeQueue(st *store.Store, ws []jira.PendingWrite) {
	if len(ws) == 0 {
		_ = st.DeleteMeta(queueMeta)
		return
	}
	b, _ := json.Marshal(ws)
	_ = st.SetMeta(queueMeta, string(b))
}

// queueTo is the client's queue: the state file.
func queueTo(st *store.Store) func(jira.PendingWrite) {
	return func(w jira.PendingWrite) {
		queueMu.Lock()
		defer queueMu.Unlock()
		writeQueue(st, append(readQueue(st), w))
	}
}

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
		queueMu.Lock()
		defer queueMu.Unlock()
		ws := readQueue(st)
		var msg queueReplayedMsg
		for len(ws) > 0 {
			w := ws[0]
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if !force {
				changed, err := c.ChangedSince(cctx, w.Key(), w.At)
				if err != nil { // offline still, or unreadable: try later
					cancel()
					msg.err = err
					break
				}
				if changed {
					cancel()
					msg.conflict = w.Key()
					break
				}
			}
			err := c.Replay(cctx, w)
			cancel()
			if errors.Is(err, jira.ErrQueued) {
				break
			}
			if err != nil {
				msg.failed = append(msg.failed, w.What+": "+err.Error())
			} else {
				msg.sent++
			}
			ws, force = ws[1:], false
		}
		writeQueue(st, ws)
		msg.left = len(ws)
		return msg
	}
}

func (m Model) handleQueueReplayed(msg queueReplayedMsg) (tea.Model, tea.Cmd) {
	m.queued = msg.left
	for _, f := range msg.failed {
		m.fail("queued write refused, dropped: " + f)
	}
	switch {
	case msg.conflict != "":
		m.status = fmt.Sprintf("%s changed in Jira since you queued a write · %s → queue sends it anyway or drops it", msg.conflict, helpKey(m.keys.Palette))
	case msg.sent > 0:
		m.status = fmt.Sprintf("back online: sent %s", plural(msg.sent, "queued write"))
		if msg.left > 0 {
			m.status += fmt.Sprintf(", %d left", msg.left)
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
	m.startJiraPicker(jiraPickQueue, "Offline writes", false)
	items := []jiraPickerItem{{id: "send", label: "Send them now (also over changes made in Jira since)"}}
	for i, w := range readQueue(m.store) {
		items = append(items, jiraPickerItem{id: strconv.Itoa(i), label: fmt.Sprintf("%s  %s %s  · %s ago · enter twice drops it", w.What, w.Method, w.Path, age(w.At))})
	}
	m.setJiraPickerItems(items)
}

func (m Model) applyQueuePick(it jiraPickerItem) (tea.Model, tea.Cmd) {
	if it.id == "send" {
		m.closeJiraPicker()
		m.status = "sending the queued writes…"
		return m, m.replayQueue(true)
	}
	if m.jiraPicker.pendingDelete != it.id {
		m.jiraPicker.pendingDelete = it.id
		m.status = "enter again drops this write"
		return m, nil
	}
	m.closeJiraPicker()
	i, _ := strconv.Atoi(it.id)
	queueMu.Lock()
	ws := readQueue(m.store)
	if i < len(ws) {
		ws = append(ws[:i], ws[i+1:]...)
		writeQueue(m.store, ws)
	}
	queueMu.Unlock()
	m.queued = len(ws)
	m.status = "dropped the write"
	return m, nil
}
