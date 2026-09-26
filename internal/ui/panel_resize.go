package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// Dragging the panel's left border resizes it, within ui.panel_width's
// bounds; the width is remembered once the button lets go. Near
// ui.panel_width it snaps to it, and letting go there forgets the drag.

const panelWidthMeta = jiraMetaPrefix + "panel_width"

// panelSnap is how many percent from ui.panel_width a drag snaps to it.
const panelSnap = 3

// resizePanel puts the panel's left border at screen column x.
func (m Model) resizePanel(x int) (tea.Model, tea.Cmd) {
	if m.width <= 0 {
		return m, nil
	}
	pct := min(max((m.width-x)*100/m.width, 20), 80)
	def := m.opts.panelDefault
	if pct-def <= panelSnap && def-pct <= panelSnap {
		pct = def
	}
	if pct == m.opts.panelPct {
		return m, nil
	}
	m.opts.panelPct = pct
	m.status = "panel " + strconv.Itoa(pct) + "%"
	if pct == def {
		m.status += " (ui.panel_width)"
	}
	m.resize()
	return m, nil
}

// panelStep is how many percent < and > move the panel's border.
const panelStep = 5

// stepPanel widens (d > 0) or narrows the open panel by a step, stopping at
// ui.panel_width on the way past it, and remembers the width.
func (m *Model) stepPanel(d int) {
	if !m.refOpen {
		return
	}
	pct, def := m.opts.panelPct, m.opts.panelDefault
	next := min(max(pct+d*panelStep, 20), 80)
	if (pct < def) != (next < def) && pct != def && next != def {
		next = def
	}
	m.opts.panelPct = next
	m.status = "panel " + strconv.Itoa(next) + "%"
	if next == def {
		m.status += " (ui.panel_width)"
	}
	m.savePanelWidth()
	m.resize()
}

// loadPanelWidth takes the remembered width over ui.panel_width.
func (m *Model) loadPanelWidth() {
	if m.store == nil {
		return
	}
	if v, ok, _ := m.store.GetMeta(panelWidthMeta); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 20 && n <= 80 {
			m.opts.panelPct = n
		}
	}
}

// savePanelWidth remembers a dragged width, or forgets it back at
// ui.panel_width.
func (m *Model) savePanelWidth() {
	switch {
	case m.store == nil:
	case m.opts.panelPct == m.opts.panelDefault:
		_ = m.store.DeleteMeta(panelWidthMeta)
	default:
		_ = m.store.SetMeta(panelWidthMeta, strconv.Itoa(m.opts.panelPct))
	}
}
