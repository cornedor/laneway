package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"  // attachment formats
	_ "image/jpeg" // attachment formats
	_ "image/png"  // attachment formats and the kitty transmit format
	"math/rand/v2"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/cornedor/laneway/internal/jira"
)

// Attachment images in the panel, drawn with the kitty graphics protocol's
// Unicode placeholders: each image is transmitted once (tea.Raw) with a
// virtual placement, then shown by placeholder cells whose foreground carries
// its id. The cells are ordinary text, so the image scrolls with the panel.
// Kitty and Ghostty support it; elsewhere the caption stays text.

const (
	imgMaxPx = 1600 // longer sides are downscaled before transmitting
)

// cellPx is a terminal cell's size in pixels.
type cellPx struct{ w, h int }

// defaultCell is the common 1:2 of a terminal font, until the terminal
// answers CSI 16 t.
var defaultCell = cellPx{10, 20}

type imgState int

const (
	imgLoading imgState = iota
	imgReady
	imgFailed
)

type panelImage struct {
	state      imgState
	id         uint32 // kitty image id, 24-bit
	pxW, pxH   int    // the transmitted pixels, for re-fitting
	cols, rows int    // the current placement
}

// panelImages holds the panel's images by attachment id, for the session.
type panelImages struct {
	on      bool
	maxRows int    // tallest an image is drawn, in cells
	cell    cellPx // the terminal's cell size, for the aspect
	nextID  uint32
	byAtt   map[string]*panelImage
	// pending is placement changes a render queued, flushed after Update.
	pending strings.Builder
}

func newPanelImages(on bool, maxRows int) *panelImages {
	return &panelImages{on: on && kittyGraphics(), maxRows: maxRows, cell: defaultCell, nextID: 1 + rand.Uint32N(1<<20), byAtt: map[string]*panelImage{}}
}

// kittyGraphics reports a terminal that draws Unicode placeholders. tmux
// would need passthrough, so it stays off there.
func kittyGraphics() bool {
	if os.Getenv("LANEWAY_IMAGES") == "0" || os.Getenv("TMUX") != "" {
		return false
	}
	term, prog := os.Getenv("TERM"), os.Getenv("TERM_PROGRAM")
	return os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" || term == "xterm-ghostty" || strings.EqualFold(prog, "ghostty")
}

// imageLoadedMsg carries one fetched image, encoded for transmit.
type imageLoadedMsg struct {
	att        string
	id         uint32
	pxW, pxH   int
	cols, rows int
	seq        string
	err        error
}

// fetchIssueImages starts downloads for the issue's image attachments not
// fetched yet, sized to the panel width.
func (m *Model) fetchIssueImages(iss *jira.Issue) tea.Cmd {
	ii := m.images
	if ii == nil || !ii.on || iss == nil {
		return nil
	}
	box := max(m.refView.Width()-4, 8)
	var cmds []tea.Cmd
	for _, a := range iss.Attachments {
		if !a.IsImage() || ii.byAtt[a.ID] != nil || !issueShowsAttachment(iss, a.ID) {
			continue
		}
		id := ii.nextID & 0xFFFFFF
		ii.nextID++
		ii.byAtt[a.ID] = &panelImage{state: imgLoading, id: id}
		ctx, c, att, maxRows, cell := m.ctx, m.jiraClient, a.ID, ii.maxRows, ii.cell
		cmds = append(cmds, func() tea.Msg {
			b, err := c.AttachmentContent(ctx, att)
			if err != nil {
				return imageLoadedMsg{att: att, err: err}
			}
			seq, w, h, err := encodeKittyImage(id, b, box, maxRows, cell)
			cols, rows := fitCells(w, h, box, maxRows, cell)
			return imageLoadedMsg{att: att, id: id, pxW: w, pxH: h, cols: cols, rows: rows, seq: seq, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// issueShowsAttachment reports whether the description or a comment embeds
// attachment att, so files only listed on the issue aren't downloaded.
func issueShowsAttachment(iss *jira.Issue, att string) bool {
	ref := "](" + jira.AttachmentScheme + att + ")"
	if strings.Contains(iss.Description, ref) {
		return true
	}
	for _, c := range iss.Comments {
		if strings.Contains(c.Body, ref) {
			return true
		}
	}
	return false
}

func (m Model) handleImageLoaded(msg imageLoadedMsg) (tea.Model, tea.Cmd) {
	e := m.images.byAtt[msg.att]
	if e == nil {
		return m, nil
	}
	if msg.err != nil {
		e.state = imgFailed
		return m, nil
	}
	e.state, e.pxW, e.pxH, e.cols, e.rows = imgReady, msg.pxW, msg.pxH, msg.cols, msg.rows
	m.renderRef()
	return m, tea.Raw(msg.seq)
}

// encodeKittyImage decodes b, downscales it past imgMaxPx, fits it to at most
// box columns and imgMaxRows rows, and builds the transmit sequence. w×h is
// the transmitted pixel size.
func encodeKittyImage(id uint32, b []byte, box, maxRows int, cell cellPx) (seq string, w, h int, err error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", 0, 0, fmt.Errorf("decode image: %w", err)
	}
	img = shrinkImage(img, imgMaxPx)
	w, h = img.Bounds().Dx(), img.Bounds().Dy()
	cols, rows := fitCells(w, h, box, maxRows, cell)
	var sb strings.Builder
	err = kitty.EncodeGraphics(&sb, img, &kitty.Options{
		Action:           kitty.TransmitAndPut,
		VirtualPlacement: true,
		ID:               int(id),
		Rows:             rows,
		Columns:          cols,
		Format:           kitty.PNG,
		Transmission:     kitty.Direct,
		Quite:            2,
		Chunk:            true,
	})
	return sb.String(), w, h, err
}

// queryCellSize asks the terminal its cell size in pixels (CSI 16 t); the
// answer arrives as a uv.CellSizeEvent.
func (m *Model) queryCellSize() tea.Cmd {
	if m.images == nil || !m.images.on {
		return nil
	}
	return tea.Raw("\x1b[16t")
}

// handleCellSize re-fits the images to the terminal's real cell size; Update
// flushes the moved placements.
func (m Model) handleCellSize(msg uv.CellSizeEvent) (tea.Model, tea.Cmd) {
	if m.images == nil || msg.Width <= 0 || msg.Height <= 0 {
		return m, nil
	}
	m.images.cell = cellPx{msg.Width, msg.Height}
	m.renderRef()
	m.fitImageView()
	return m, nil
}

// refit moves e's virtual placement to rows×cols, keeping the image data.
func (ii *panelImages) refit(e *panelImage, cols, rows int) {
	if e.cols == cols && e.rows == rows {
		return
	}
	e.cols, e.rows = cols, rows
	fmt.Fprintf(&ii.pending, "\x1b_Ga=d,d=i,i=%d,q=2\x1b\\\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", e.id, e.id, cols, rows)
}

// flushImages sends the placement changes renders queued.
func (m *Model) flushImages() tea.Cmd {
	if m.images == nil || m.images.pending.Len() == 0 {
		return nil
	}
	seq := m.images.pending.String()
	m.images.pending.Reset()
	return tea.Raw(seq)
}

// fitCells sizes a w×h pixel image in cells: aspect kept, no upscaling, at
// most box columns and maxRows rows.
func fitCells(w, h, box, maxRows int, cell cellPx) (cols, rows int) {
	if w <= 0 || h <= 0 {
		return 1, 1
	}
	if cell.w <= 0 || cell.h <= 0 {
		cell = defaultCell
	}
	cols = min(box, (w+cell.w-1)/cell.w)
	rows = (cols*cell.w*h/w + cell.h - 1) / cell.h
	if rows > maxRows {
		rows = maxRows
		cols = rows * cell.h * w / (h * cell.w)
	}
	return max(cols, 1), max(rows, 1)
}

// shrinkImage scales img down by an integer box filter until its longer side
// is at most maxPx.
func shrinkImage(img image.Image, maxPx int) image.Image {
	b := img.Bounds()
	f := (max(b.Dx(), b.Dy()) + maxPx - 1) / maxPx
	if f <= 1 {
		return img
	}
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()/f, b.Dy()/f))
	n := uint32(f * f)
	for y := 0; y < dst.Rect.Dy(); y++ {
		for x := 0; x < dst.Rect.Dx(); x++ {
			var r, g, bl, a uint32
			for dy := 0; dy < f; dy++ {
				o := src.PixOffset(x*f, y*f+dy)
				for dx := 0; dx < f; dx++ {
					p := src.Pix[o+dx*4 : o+dx*4+4]
					r, g, bl, a = r+uint32(p[0]), g+uint32(p[1]), bl+uint32(p[2]), a+uint32(p[3])
				}
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
		}
	}
	return dst
}

// kittyPlaceholder is the text that shows image id over rows×cols cells. The
// id foreground is reopened on every row, since the pane's layout resets
// styles at line ends.
func kittyPlaceholder(id uint32, rows, cols int) []string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", byte(id>>16), byte(id>>8), byte(id))
	lines := make([]string, rows)
	for r := range rows {
		var sb strings.Builder
		sb.WriteString(fg)
		for c := range cols {
			sb.WriteRune(kitty.Placeholder)
			sb.WriteRune(kitty.Diacritic(r))
			sb.WriteRune(kitty.Diacritic(c))
		}
		sb.WriteString("\x1b[39m")
		lines[r] = sb.String()
	}
	return lines
}

// placeImages strips the image marks from rendered panel content and puts a
// ready image's placeholder rows under its caption.
func (m *Model) placeImages(s string) string {
	if !strings.Contains(s, imgMarkPrefix) {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		var atts []string
		for {
			i := strings.Index(l, imgMarkPrefix)
			if i < 0 {
				break
			}
			j := strings.Index(l[i:], imgMarkEnd)
			if j < 0 {
				break
			}
			atts = append(atts, l[i+len(imgMarkPrefix):i+j])
			l = l[:i] + l[i+j+len(imgMarkEnd):]
		}
		out = append(out, l)
		indent := strings.Repeat(" ", len(l)-len(strings.TrimLeft(l, " ")))
		for _, a := range atts {
			if e := m.images.ready(a); e != nil {
				cols, rows := fitCells(e.pxW, e.pxH, max(m.refView.Width()-len(indent), 1), m.images.maxRows, m.images.cell)
				if !m.imageView { // it holds the shown image's placement
					m.images.refit(e, cols, rows)
				}
				for _, row := range kittyPlaceholder(e.id, e.rows, e.cols) {
					out = append(out, indent+row)
				}
			}
		}
	}
	return strings.Join(out, "\n")
}

func (ii *panelImages) ready(att string) *panelImage {
	if ii == nil {
		return nil
	}
	if e := ii.byAtt[att]; e != nil && e.state == imgReady {
		return e
	}
	return nil
}

// ReleaseImages is the sequence that frees every image this session sent,
// by id so other programs' images stay; "" when none were sent. Write it to
// the terminal after the program exits.
func (m Model) ReleaseImages() string {
	if m.images == nil {
		return ""
	}
	var sb strings.Builder
	for _, e := range m.images.byAtt {
		if e.state == imgReady {
			fmt.Fprintf(&sb, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", e.id)
		}
	}
	return sb.String()
}
