package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/textwidth"
)

func TestFitCells(t *testing.T) {
	for _, c := range []struct{ w, h, box, cols, rows int }{
		{1000, 200, 80, 80, 8},   // wide: box-limited
		{100, 100, 80, 10, 5},    // small: native size, no upscale
		{2000, 1000, 80, 64, 16}, // wide: height cap narrows it
		{400, 4000, 80, 3, 16},   // tall
		{0, 5, 80, 1, 1},
	} {
		cols, rows := fitCells(c.w, c.h, c.box, 16, defaultCell)
		if cols != c.cols || rows != c.rows {
			t.Errorf("fitCells(%d,%d,%d) = %d×%d, want %d×%d", c.w, c.h, c.box, cols, rows, c.cols, c.rows)
		}
	}
}

func TestShrinkImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4000, 1000))
	for i := range src.Pix {
		src.Pix[i] = 200
	}
	got := shrinkImage(src, 1600)
	if b := got.Bounds(); b.Dx() != 1333 || b.Dy() != 333 {
		t.Fatalf("shrunk to %v", b)
	}
	if c := got.At(10, 10).(color.RGBA); c.R != 200 {
		t.Errorf("pixel = %v, want averaged 200", c)
	}
	if small := image.NewRGBA(image.Rect(0, 0, 10, 10)); shrinkImage(small, 1600) != image.Image(small) {
		t.Error("small image was copied")
	}
}

func TestKittyPlaceholderWidth(t *testing.T) {
	rows := kittyPlaceholder(0x123456, 3, 7)
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	for i, r := range rows {
		if w := textwidth.Width(r); w != 7 {
			t.Errorf("row %d width = %d, want 7", i, w)
		}
		if !strings.HasPrefix(r, "\x1b[38;2;18;52;86m") {
			t.Errorf("row %d lacks the id foreground: %q", i, r[:20])
		}
	}
}

func TestEncodeKittyImage(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 50, 40)))
	seq, w, h, err := encodeKittyImage(7, buf.Bytes(), 80, 16, defaultCell)
	if err != nil || w != 50 || h != 40 {
		t.Fatalf("encode: %d×%d %v", w, h, err)
	}
	if !strings.Contains(seq, "c=5") || !strings.Contains(seq, "r=2") {
		t.Errorf("placement not 5×2: %.80q", seq)
	}
	if !strings.HasPrefix(seq, "\x1b_G") || !strings.Contains(seq, "i=7") || !strings.Contains(seq, "U=1") {
		t.Errorf("seq = %.60q", seq)
	}
	if _, _, _, err := encodeKittyImage(7, []byte("nope"), 80, 16, defaultCell); err == nil {
		t.Error("garbage decoded")
	}
}

// TestPanelPlacesImage: a ready attachment's placeholder rows follow its
// caption, the mark never reaches the screen, and a non-attachment image gets
// no mark at all.
func TestPanelPlacesImage(t *testing.T) {
	m := jiraTabModel(t)
	m.images = &panelImages{on: true, maxRows: 16, byAtt: map[string]*panelImage{}}
	iss := &jira.Issue{Key: "ABC-1", Summary: "s",
		Description: "![shot.png](attachment:10)\n\n![web](https://x.test/a.png)",
		Attachments: []jira.Attachment{{ID: "10", Filename: "shot.png", MimeType: "image/png"}, {ID: "11", MimeType: "image/png"}}}
	out, _ := openRefFor(m, "ABC-1")
	m = out.(Model)
	out, cmd := m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: iss})
	m = out.(Model)
	if cmd == nil || m.images.byAtt["10"] == nil || m.images.byAtt["11"] != nil {
		t.Fatalf("fetch: cmd=%v queued=%v", cmd != nil, m.images.byAtt)
	}
	id := m.images.byAtt["10"].id
	out, raw := m.handleImageLoaded(imageLoadedMsg{att: "10", id: id, pxW: 40, pxH: 40, cols: 4, rows: 2, seq: "SEQ"})
	m = out.(Model)
	if raw == nil {
		t.Error("no transmit")
	}
	view := m.refView.View()
	if strings.Contains(view, imgMarkPrefix) || strings.Contains(view, "5379") {
		t.Error("image mark leaked to the screen")
	}
	if n := strings.Count(view, string(rune(0x10EEEE))); n != 8 {
		t.Errorf("placeholder cells = %d, want 8", n)
	}
	full := m.View().Content
	if n := strings.Count(full, string(rune(0x10EEEE))); n != 8 {
		t.Errorf("full view placeholder cells = %d, want 8", n)
	}
	if !strings.Contains(full, fmt.Sprintf("\x1b[38;2;%d;%d;%dm", byte(id>>16), byte(id>>8), byte(id))) {
		t.Error("id foreground lost in the pane layout")
	}
	if !strings.Contains(view, "shot.png") || !strings.Contains(view, "web") {
		t.Error("captions missing")
	}
	// A square-celled terminal answers CSI 16 t: the 40×40 image re-fits
	// to 2×2 cells, the placement moved without resending the data.
	out, raw = m.handleCellSize(uv.CellSizeEvent{Width: 20, Height: 20})
	m = out.(Model)
	if n := strings.Count(m.refView.View(), string(rune(0x10EEEE))); n != 4 || raw == nil {
		t.Errorf("after cell size: placeholder cells = %d, flush %v", n, raw != nil)
	}
}

func TestReleaseImages(t *testing.T) {
	m := Model{images: &panelImages{byAtt: map[string]*panelImage{
		"1": {state: imgReady, id: 42}, "2": {state: imgLoading, id: 43},
	}}}
	if got := m.ReleaseImages(); got != "\x1b_Ga=d,d=I,i=42,q=2\x1b\\" {
		t.Errorf("release = %q", got)
	}
	if (Model{}).ReleaseImages() != "" {
		t.Error("no images should release nothing")
	}
}

// TestImageRefitsToNarrowPanel: a panel too narrow for the placement moves it
// to a smaller one, sent after the update.
func TestImageRefitsToNarrowPanel(t *testing.T) {
	m := jiraTabModel(t)
	m.images = &panelImages{on: true, maxRows: 16, byAtt: map[string]*panelImage{"10": {state: imgReady, id: 9, pxW: 800, pxH: 100, cols: 80, rows: 5}}}
	m.refView.SetWidth(42)
	got := m.placeImages("  " + imgMark("attachment:10") + "shot")
	if e := m.images.byAtt["10"]; e.cols != 40 || e.rows != 3 {
		t.Fatalf("placement = %d×%d, want 40×3", e.cols, e.rows)
	}
	if n := strings.Count(got, string(rune(0x10EEEE))); n != 120 {
		t.Errorf("cells = %d, want 120", n)
	}
	if m.flushImages() == nil || m.images.pending.Len() != 0 {
		t.Error("refit not flushed")
	}
	m.placeImages("  " + imgMark("attachment:10") + "shot")
	if m.flushImages() != nil {
		t.Error("unchanged width re-placed the image")
	}
}

func TestPanelListsLooseAttachments(t *testing.T) {
	m := jiraTabModel(t)
	iss := &jira.Issue{Key: "ABC-1", Description: "![a.png](attachment:1)",
		Attachments: []jira.Attachment{{ID: "1", Filename: "a.png", MimeType: "image/png"}, {ID: "2", Filename: "spec.pdf", Size: 3 << 20}}}
	out := ansi.Strip(m.renderJiraIssue(iss, 60))
	if !strings.Contains(out, "Attachments (1)") || !strings.Contains(out, "spec.pdf  3.0 MB") {
		t.Errorf("attachments section missing:\n%s", out)
	}
	if strings.Count(out, "a.png") != 1 {
		t.Error("embedded image listed again")
	}
	for n, want := range map[int64]string{500: "500 B", 2048: "2.0 KB", 50 << 20: "50 MB"} {
		if got := byteSize(n); got != want {
			t.Errorf("byteSize(%d) = %q, want %q", n, got, want)
		}
	}
}
