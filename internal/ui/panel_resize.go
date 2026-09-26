package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// Dragging the panel's left border resizes it, within ui.panel_width's
// bounds; the width is remembered once the button lets go.

const panelWidthMeta = jiraMetaPrefix + "panel_width"

// resizePanel puts the panel's left border at screen column x.
func (m Model) resizePanel(x int) (tea.Model, tea.Cmd) {
	if m.width <= 0 {
		return m, nil
	}
	pct := min(max((m.width-x)*100/m.width, 20), 80)
	if pct == m.opts.panelPct {
		return m, nil
	}
	m.opts.panelPct = pct
	m.status = "panel " + strconv.Itoa(pct) + "%"
	m.resize()
	return m, nil
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

func (m *Model) savePanelWidth() {
	if m.store != nil {
		_ = m.store.SetMeta(panelWidthMeta, strconv.Itoa(m.opts.panelPct))
	}
}
