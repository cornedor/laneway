package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.Black)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestAvatars: a kitty terminal gets each assignee's avatar once, kept on
// disk; the chip becomes its placeholder, the selected card keeps it, and
// the next session reads the cache.
func TestAvatars(t *testing.T) {
	dir := t.TempDir()
	orig := avatarCacheDir
	avatarCacheDir = func() string { return dir }
	t.Cleanup(func() { avatarCacheDir = orig; avatarPlace = map[string]string{}; avatars = map[string]string{} })
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write(testPNG(t))
	}))
	defer srv.Close()

	m := jiraTabModel(t)
	m.images.on = true
	m.jiraClient = jira.New(jira.Config{BaseURL: "https://x.atlassian.net", Email: "me@x.test", APIToken: "tok"})
	m.jiraTab.cards[0].AvatarURL = srv.URL + "/ada.png" // ABC-1, Ada, selected
	m.jiraTab.cards[1].Assignee, m.jiraTab.cards[1].AvatarURL = "Ada", srv.URL+"/ada.png"
	cmd := m.fetchAvatars()
	if cmd == nil || m.fetchAvatars() != nil {
		t.Fatal("want one fetch per avatar")
	}
	msg := cmd().(avatarLoadedMsg)
	if msg.err != nil || !strings.Contains(msg.seq, "\x1b_G") {
		t.Fatalf("msg = %+v", msg)
	}
	out, raw := m.handleAvatarLoaded(msg)
	m = out.(Model)
	if raw == nil || !strings.Contains(jiraAvatar("Ada"), "\U0010EEEE") {
		t.Fatal("chip is not the avatar")
	}
	view := m.View().Content
	if strings.Count(view, "\U0010EEEE") < 4 { // two cards, two cells each
		t.Errorf("avatars on the board: %d cells", strings.Count(view, "\U0010EEEE"))
	}
	if files, _ := os.ReadDir(dir); len(files) != 1 || hits != 1 {
		t.Errorf("cache %d files, %d downloads", len(files), hits)
	}
	// A new session reads the disk.
	m2 := jiraTabModel(t)
	m2.images.on = true
	m2.jiraClient = m.jiraClient
	m2.jiraTab.cards[0].AvatarURL = srv.URL + "/ada.png"
	if msg := m2.fetchAvatars()().(avatarLoadedMsg); msg.err != nil || hits != 1 {
		t.Errorf("cached: %v, %d downloads", msg.err, hits)
	}
}

func TestStripKeepImages(t *testing.T) {
	img := kittyPlaceholder(0x010203, 1, 2)[0]
	in := "\x1b[1mABC-1\x1b[0m " + img + "\x1b[2m Ada\x1b[0m"
	got := stripKeepImages(in)
	if !strings.Contains(got, img) || ansi.Strip(got) != ansi.Strip(in) || strings.Contains(got, "\x1b[1m") {
		t.Errorf("got %q", got)
	}
}
