package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Clicks in the overlays drawn centred over the board: settings, the
// filter builder, the JQL search, go-to and create. A click on a row picks
// it, outside the box closes, as their esc would. The composers keep their
// text: a stray click doesn't cancel them. The image viewer steps by half.

// overlayAt places box as the view centres it and says whether x, y is on
// it; top and left are its outer corner.
func (m *Model) overlayAt(box string, x, y int) (top, left int, inside bool) {
	bodyH := m.bodyH()
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	top, left = max((bodyH-h)/2, 0), max((m.width-w)/2, 0) // as the view clamps a box too big
	return top, left, y >= top && y < top+h && x >= left && x < left+w
}

// The overlays' content starts inside the border and padding (1, 3).
const overlayPadTop, overlayPadLeft = 2, 4

// builderOnTop and jqlOnTop are whether that overlay is the one drawn.
func (m *Model) builderOnTop() bool { return m.filterBuilder != nil && m.settings == nil }

func (m *Model) jqlOnTop() bool {
	return m.jql != nil && m.underInputs()
}

// underInputs is whether no overlay routed before the image viewer and the
// one-line inputs (JQL, go-to, create) is open.
func (m *Model) underInputs() bool {
	return m.settings == nil && m.filterBuilder == nil && m.descEdit == nil && !m.helpOpen
}

// imageOnTop, gotoOnTop and createOnTop are whether that one has the keys.
func (m *Model) imageOnTop() bool { return m.imageView && m.underInputs() }

func (m *Model) gotoOnTop() bool {
	return m.jiraGotoActive && !m.imageView && m.jql == nil && m.underInputs()
}

func (m *Model) createOnTop() bool {
	return m.jiraCreateActive && !m.imageView && m.jql == nil && !m.jiraGotoActive && m.underInputs()
}

// clickOverlay handles a left click on settings, the builder or the JQL
// search; ok is false when none of them is on top.
func (m Model) clickOverlay(x, y, count int) (out tea.Model, cmd tea.Cmd, ok bool) {
	switch {
	case m.settings != nil:
		out, cmd = m.clickSettings(x, y, count)
	case m.builderOnTop():
		out, cmd = m.clickFilterBuilder(x, y)
	case m.imageOnTop():
		out, cmd = m.clickImageView(x, y)
	case m.jqlOnTop():
		out, cmd = m.clickJQL(x, y)
	case m.gotoOnTop():
		if _, _, inside := m.overlayAt(m.renderJiraGoto(), x, y); !inside {
			out, cmd = m.handleJiraGotoKey(keyPress("esc"))
		} else {
			out = m
		}
	case m.createOnTop():
		if _, _, inside := m.overlayAt(m.renderJiraCreate(), x, y); !inside {
			out, cmd = m.handleJiraCreateKey(keyPress("esc"))
		} else {
			out = m
		}
	default:
		return m, nil, false
	}
	return out, cmd, true
}

// clickImageView steps to the previous image on the left half, the next on
// the right; below the image (its caption) goes back.
func (m Model) clickImageView(x, y int) (tea.Model, tea.Cmd) {
	back := keyPress("esc")
	ids := m.readyImages()
	if len(ids) == 0 {
		return m.handleImageViewKey(back)
	}
	rows := m.images.ready(ids[min(m.imageViewIdx, len(ids)-1)]).rows
	switch top := (m.bodyH() - rows - 2) / 2; { // the image, a gap, the caption
	case y >= top+rows:
		return m.handleImageViewKey(back)
	case x < m.width/2:
		return m.handleImageViewKey(keyPress("left"))
	}
	return m.handleImageViewKey(keyPress("right"))
}

// clickSettings selects a row; a click on the selected one edits it.
// While editing, a click outside the box cancels the edit.
func (m Model) clickSettings(x, y, count int) (tea.Model, tea.Cmd) {
	s := m.settings
	top, _, inside := m.overlayAt(m.renderSettings(m.bodyH()), x, y)
	switch {
	case s.input != nil && !inside:
		return m.handleSettingsInput(keyPress("esc"))
	case s.input != nil:
		return m, nil
	case !inside:
		m.settings = nil
		return m, nil
	}
	first, visible := s.window(m.bodyH())
	r := y - top - overlayPadTop - 3 // the title, its rule and the column heads
	if r < 0 || r >= visible || first+r >= len(s.rows) {
		return m, nil
	}
	if i := first + r; i != s.idx {
		s.idx = i
		return m, nil
	}
	m.editSetting()
	return m, nil
}

// clickFilterBuilder moves to the clicked column; a row picks its value
// and goes on to the next column, a value adds the term.
func (m Model) clickFilterBuilder(x, y int) (tea.Model, tea.Cmd) {
	b := m.filterBuilder
	top, left, inside := m.overlayAt(m.renderFilterBuilder(m.bodyH()), x, y)
	if !inside {
		m.filterBuilder = nil
		return m, nil
	}
	col, at := -1, left+overlayPadLeft
	for c, w := range builderWidths {
		if x >= at && x < at+w {
			col = c
		}
		at += w + 2
	}
	line := y - top - overlayPadTop - 5 // title, query, input and their gaps
	if col < 0 || line < 0 {
		return m, nil
	}
	rows := m.builderRows(col)
	first, visible := b.window(col, len(rows), m.bodyH())
	if col != b.col {
		b.col = col
		b.filter.SetValue("")
	}
	i := first + line - 1
	if line == 0 || line > visible || i >= len(rows) {
		return m, nil // the column's head, or below its rows
	}
	b.idx[col] = i
	for c := col + 1; c < 3; c++ {
		b.idx[c] = 0
	}
	return m.handleFilterBuilderKey(keyPress("enter"))
}

// clickJQL completes with the clicked completion, as tab would.
func (m Model) clickJQL(x, y int) (tea.Model, tea.Cmd) {
	j := m.jql
	top, _, inside := m.overlayAt(m.renderJQL(), x, y)
	if !inside {
		return m.handleJQLKey(keyPress("esc"))
	}
	r := y - top - overlayPadTop - 4 // title, input and their gaps
	if i := j.top() + r; r >= 0 && r < jqlShown && i < len(j.sugg) {
		j.idx = i
		return m.handleJQLKey(keyPress("tab"))
	}
	return m, nil
}

// wheelOverlay moves the cursor of the overlay on top; ok is false when
// it takes no wheel.
func (m Model) wheelOverlay(up bool) (tea.Model, tea.Cmd, bool) {
	k := keyPress("down")
	if up {
		k = keyPress("up")
	}
	switch {
	case m.settings != nil && m.settings.input == nil:
		out, cmd := m.handleSettingsKey(k)
		return out, cmd, true
	case m.builderOnTop():
		out, cmd := m.handleFilterBuilderKey(k)
		return out, cmd, true
	case m.imageOnTop():
		k = keyPress("right")
		if up {
			k = keyPress("left")
		}
		out, cmd := m.handleImageViewKey(k)
		return out, cmd, true
	case m.jqlOnTop():
		out, cmd := m.handleJQLKey(k)
		return out, cmd, true
	}
	return m, nil, false
}
