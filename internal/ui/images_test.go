package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"jiratui/internal/jira"
	"jiratui/internal/textwidth"
)

func TestFitCells(t *testing.T) {
	for _, c := range []struct{ w, h, box, cols, rows int }{
		{1000, 200, 80, 80, 8},   // wide: box-limited
		{100, 100, 80, 10, 5},    // small: native size, no upscale
		{2000, 1000, 80, 64, 16}, // wide: height cap narrows it
		{400, 4000, 80, 3, 16},   // tall
		{0, 5, 80, 1, 1},
	} {
		cols, rows := fitCells(c.w, c.h, c.box, imgMaxRows)
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
	seq, cols, rows, err := encodeKittyImage(7, buf.Bytes(), 80)
	if err != nil || cols != 5 || rows != 2 {
		t.Fatalf("encode: %d×%d %v", cols, rows, err)
	}
	if !strings.HasPrefix(seq, "\x1b_G") || !strings.Contains(seq, "i=7") || !strings.Contains(seq, "U=1") {
		t.Errorf("seq = %.60q", seq)
	}
	if _, _, _, err := encodeKittyImage(7, []byte("nope"), 80); err == nil {
		t.Error("garbage decoded")
	}
}

// TestPanelPlacesImage: a ready attachment's placeholder rows follow its
// caption, the mark never reaches the screen, and a non-attachment image gets
// no mark at all.
func TestPanelPlacesImage(t *testing.T) {
	m := jiraTabModel(t)
	m.images = &panelImages{on: true, byAtt: map[string]*panelImage{}}
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
	out, raw := m.handleImageLoaded(imageLoadedMsg{att: "10", id: id, cols: 4, rows: 2, seq: "SEQ"})
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
