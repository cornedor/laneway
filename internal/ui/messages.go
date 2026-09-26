package ui

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The status line's messages, kept: the line shows one and cuts it to the
// screen, the next replaces it; the palette's messages row lists the last
// ones with their time, enter copies one whole.

const statusLogMax = 100

type statusEntry struct {
	at   time.Time
	text string
	err  bool
}

// fail puts an error on the status line: drawn in the theme's error colour
// while it shows, marked in the log.
func (m *Model) fail(s string) {
	m.status, m.statusErr = s, s
}

// statusIsErr is whether the status line shows an error.
func (m *Model) statusIsErr() bool { return m.status != "" && m.status == m.statusErr }

// logStatus keeps the status line's message when it is a new one.
func (m *Model) logStatus() {
	if m.status == "" || m.status == m.statusLogged {
		return
	}
	m.statusLogged = m.status
	m.statusLog = append(m.statusLog, statusEntry{time.Now(), m.status, m.statusIsErr()})
	if n := len(m.statusLog); n > statusLogMax {
		m.statusLog = slices.Delete(m.statusLog, 0, n-statusLogMax)
	}
}

// startupStatus is what the status line says about the config's warnings,
// each kept in the log: one as it is, more as a count.
func (m *Model) startupStatus(warn []string) {
	m.warnings, m.statusLog = warn, nil
	for _, w := range warn {
		m.statusLog = append(m.statusLog, statusEntry{time.Now(), w, true})
	}
	switch len(warn) {
	case 0:
	case 1:
		m.status = warn[0]
	default:
		m.status = fmt.Sprintf("%d config warnings · %s messages lists them", len(warn), helpKey(m.keys.Palette))
	}
	m.statusLogged, m.statusErr = m.status, m.status
}

// WithWarnings adds the config file's own warnings (unknown keys) to the
// startup ones.
func (m Model) WithWarnings(warn []string) Model {
	m.startupStatus(append(slices.Clone(m.warnings), warn...))
	return m
}

// openMessages lists the kept messages, newest first.
func (m *Model) openMessages() {
	m.startJiraPicker(jiraPickMessages, "Messages · enter copies one", true)
	m.jiraPicker.filter.Placeholder = "filter messages…"
	items := make([]jiraPickerItem, 0, len(m.statusLog))
	for i := len(m.statusLog) - 1; i >= 0; i-- {
		e := m.statusLog[i]
		mark := "  "
		if e.err {
			mark = "✗ "
		}
		items = append(items, jiraPickerItem{id: strconv.Itoa(i), label: e.at.Format("15:04:05") + "  " + mark + e.text})
	}
	if len(items) == 0 {
		m.jiraPicker.err = fmt.Errorf("no messages yet")
	}
	m.setJiraPickerItems(items)
}

// applyMessage copies the picked message whole.
func (m Model) applyMessage(id string) (tea.Model, tea.Cmd) {
	i, err := strconv.Atoi(id)
	if err != nil || i >= len(m.statusLog) {
		return m, nil
	}
	text := m.statusLog[i].text
	m.status = "copied the message"
	return m, tea.SetClipboard(text)
}
