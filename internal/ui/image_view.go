package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// i in the panel: the issue's images one at a time across the whole body,
// ← → to step, any other key back. The view replaces the body rather than
// overlaying it, so only one placement of the image is on screen.

// readyImages are the open issue's drawn attachments, in its order.
func (m *Model) readyImages() []string {
	if m.jiraIssue == nil || m.images == nil {
		return nil
	}
	var out []string
	for _, a := range m.jiraIssue.Attachments {
		if m.images.ready(a.ID) != nil {
			out = append(out, a.ID)
		}
	}
	return out
}

func (m *Model) openImageView() {
	ids := m.readyImages()
	if len(ids) == 0 {
		m.status = "no images to show"
		return
	}
	m.imageView, m.imageViewIdx = true, 0
	m.fitImageView()
}

// openImageViewAt shows attachment att full size, the others a step away.
func (m *Model) openImageViewAt(att string) {
	m.openImageView()
	if i := slices.Index(m.readyImages(), att); i >= 0 && m.imageView {
		m.imageViewIdx = i
		m.fitImageView()
	}
}

// fitImageView sizes the shown image to the body; the placement change is
// queued for flushImages.
func (m *Model) fitImageView() {
	ids := m.readyImages()
	if !m.imageView || len(ids) == 0 {
		return
	}
	e := m.images.ready(ids[min(m.imageViewIdx, len(ids)-1)])
	cols, rows := fitCells(e.pxW, e.pxH, max(m.width-2, 1), max(m.bodyH()-3, 1), m.images.cell)
	m.images.refit(e, cols, rows)
}

func (m Model) handleImageViewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.readyImages())
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "right", "l":
		if n > 0 {
			m.imageViewIdx = (m.imageViewIdx + 1) % n
		}
	case "left", "h":
		if n > 0 {
			m.imageViewIdx = (m.imageViewIdx + n - 1) % n
		}
	default:
		m.imageView = false
		m.renderRef() // back to the panel's size
		return m, nil
	}
	m.fitImageView()
	return m, nil
}

// renderImageView is the current image fitted to width×height, centred,
// with its name and position.
func (m *Model) renderImageView(width, height int) string {
	ids := m.readyImages()
	if len(ids) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, refDimStyle.Render("no images"))
	}
	i := min(m.imageViewIdx, len(ids)-1)
	e := m.images.ready(ids[i])
	name := ""
	for _, a := range m.jiraIssue.Attachments {
		if a.ID == ids[i] {
			name = a.Filename
		}
	}
	caption := refDimStyle.Render(fmt.Sprintf("%s  %d/%d  ← → · any key back", name, i+1, len(ids)))
	img := strings.Join(kittyPlaceholder(e.id, e.rows, e.cols), "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, img, "", caption))
}
