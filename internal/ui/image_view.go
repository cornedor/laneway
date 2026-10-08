package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/i18n"
)

// i in the panel: the issue's images one at a time across the whole body,
// ← → to step, any other key back. The view replaces the body rather than
// overlaying it, so only one placement of the image is on screen. It shows
// the panel's copy at once and swaps in one made from the original
// (imgFullPx) when that is sent; that copy is freed when the view moves on.

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

func (m *Model) openImageView() tea.Cmd {
	ids := m.readyImages()
	if len(ids) == 0 {
		m.status = i18n.T("no images to show")
		return nil
	}
	m.imageView, m.imageViewIdx = true, 0
	return m.showImageViewImage()
}

// openImageViewAt shows attachment att full size, the others a step away.
func (m *Model) openImageViewAt(att string) tea.Cmd {
	cmd := m.openImageView()
	if i := slices.Index(m.readyImages(), att); i >= 0 && m.imageView {
		m.imageViewIdx = i
		cmd = m.showImageViewImage()
	}
	return cmd
}

// viewAtt is the attachment the image view shows, "" when none.
func (m *Model) viewAtt() string {
	ids := m.readyImages()
	if !m.imageView || len(ids) == 0 {
		return ""
	}
	return ids[min(m.imageViewIdx, len(ids)-1)]
}

// viewImage is the image the view draws: the full-size copy once it is on
// the terminal, the panel's until then.
func (m *Model) viewImage() *panelImage {
	att := m.viewAtt()
	if ii := m.images; att != "" && ii.fullAtt == att && ii.full != nil && ii.full.state == imgReady {
		return ii.full
	}
	return m.images.ready(att)
}

// showImageViewImage fits the shown image and, for a new one, frees the
// last full-size copy and starts making its own from the original.
func (m *Model) showImageViewImage() tea.Cmd {
	m.fitImageView()
	ii, att := m.images, m.viewAtt()
	if att == ii.fullAtt {
		return nil
	}
	cmd := m.dropFullImage()
	e := ii.ready(att)
	if att == "" || e.raw == nil {
		return cmd
	}
	id := ii.nextID & 0xFFFFFF
	ii.nextID++
	ii.full, ii.fullAtt = &panelImage{state: imgLoading, id: id}, att
	raw, box, maxRows, cell := e.raw, max(m.width-2, 1), max(m.bodyH()-3, 1), ii.cell
	return tea.Batch(cmd, func() tea.Msg {
		seq, w, h, err := encodeKittyImage(id, raw, imgFullPx, box, maxRows, cell)
		cols, rows := fitCells(w, h, box, maxRows, cell)
		return imageLoadedMsg{att: att, id: id, pxW: w, pxH: h, cols: cols, rows: rows, seq: seq, full: true, err: err}
	})
}

// handleFullImage puts the full-size copy on the terminal, if the view
// still wants it; a failed one leaves the panel's copy showing.
func (m Model) handleFullImage(msg imageLoadedMsg) (tea.Model, tea.Cmd) {
	e := m.images.full
	if e == nil || e.id != msg.id {
		return m, nil
	}
	if msg.err != nil {
		e.state = imgFailed
		return m, nil
	}
	e.state, e.pxW, e.pxH, e.cols, e.rows, e.seq = imgReady, msg.pxW, msg.pxH, msg.cols, msg.rows, msg.seq
	m.fitImageView()
	return m, m.images.send(msg.seq)
}

// dropFullImage frees the image view's full-size copy from the terminal.
func (m *Model) dropFullImage() tea.Cmd {
	ii := m.images
	e := ii.full
	ii.full, ii.fullAtt = nil, ""
	if e == nil || e.state != imgReady {
		return nil
	}
	return ii.send(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", e.id))
}

// fitImageView sizes the shown image to the body; the placement change is
// queued for flushImages.
func (m *Model) fitImageView() {
	e := m.viewImage()
	if e == nil {
		return
	}
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
		return m, m.dropFullImage()
	}
	return m, m.showImageViewImage()
}

// renderImageView is the current image fitted to width×height, centred,
// with its name and position.
func (m *Model) renderImageView(width, height int) string {
	ids := m.readyImages()
	if len(ids) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, refDimStyle.Render(i18n.T("no images")))
	}
	i := min(m.imageViewIdx, len(ids)-1)
	e := m.viewImage()
	name := ""
	for _, a := range m.jiraIssue.Attachments {
		if a.ID == ids[i] {
			name = a.Filename
		}
	}
	caption := refDimStyle.Render(fmt.Sprintf(i18n.T("%s  %d/%d  ← → · any key back"), name, i+1, len(ids)))
	img := strings.Join(kittyPlaceholder(e.id, e.rows, e.cols), "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, img, "", caption))
}
