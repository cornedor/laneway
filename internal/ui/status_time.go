package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
)

// A → Time in each status: how long the issue sat in each status it went
// through, all its visits added up, from its changelog.

func (m *Model) openStatusTime(key string) tea.Cmd {
	gen := m.startJiraPicker(jiraPickStatusTime, i18n.Tf("Time in status — %s", key), false)
	seq := m.jiraPicker.fetchSeq
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		now := time.Now()
		times, err := c.TimeInStatus(ctx, key, now)
		var total time.Duration
		w := 0
		for _, st := range times {
			total += st.Time
			w = max(w, len([]rune(st.Status)))
		}
		items := make([]jiraPickerItem, len(times))
		for i, st := range times {
			label := fmt.Sprintf("%-*s  %8s  %s", w, st.Status, spanText(st.Time), statusBar(st.Time, total))
			if st.Visits > 1 {
				label += fmt.Sprintf("  %d×", st.Visits)
			}
			if st.Now {
				label += i18n.T("  ← now")
			}
			items[i] = jiraPickerItem{label: label}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickStatusTime, items: items, err: err,
			title: i18n.Tf("Time in status — %s · %s since created", key, spanText(total))}
	}
}

// spanText is d as "3d 4h", "5h 20m" or "12m".
func spanText(d time.Duration) string {
	days, h, mins := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return strconv.Itoa(days) + "d " + strconv.Itoa(h) + "h"
	case h > 0:
		return strconv.Itoa(h) + "h " + strconv.Itoa(mins) + "m"
	}
	return strconv.Itoa(mins) + "m"
}

// statusBar is d's share of total as ten cells.
func statusBar(d, total time.Duration) string {
	n := 0
	if total > 0 {
		n = int(10 * d / total)
	}
	return strings.Repeat("█", n) + strings.Repeat("░", 10-n)
}
