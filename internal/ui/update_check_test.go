package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNewerSemver(t *testing.T) {
	for _, c := range []struct {
		tag, cur string
		want     bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.10.0", "v1.9.0", true},
		{"v1.2.0", "v1.2.0", false},
		{"v1.1.0", "v1.2.0", false},
		{"v1.2.0", "v1.2.0-rc1", true},
		{"v0.1.0", "v0.0.0-20260928101500-ba0922aaf6ea", true}, // a pseudo-version
		{"v1.3.0-rc1", "v1.2.0", false},                        // not a release
		{"v1.3.0", "dev", false},
		{"nonsense", "v1.2.0", false},
	} {
		if got := newerSemver(c.tag, c.cur); got != c.want {
			t.Errorf("newerSemver(%q, %q) = %v", c.tag, c.cur, got)
		}
	}
}

// fakeReleases serves tag as the latest release, counting lookups.
func fakeReleases(t *testing.T, tag string) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	t.Cleanup(srv.Close)
	orig := releaseAPI
	releaseAPI = srv.URL
	t.Cleanup(func() { releaseAPI = orig })
	return &n
}

// TestReleaseNotice: a newer release shows in the header and the palette,
// and the day's look is kept.
func TestReleaseNotice(t *testing.T) {
	n := fakeReleases(t, "v1.3.0")
	m := jiraTabModel(t).WithVersion("v1.2.0", "go install github.com/cornedor/laneway@latest")
	cmd := m.checkRelease()
	if cmd == nil {
		t.Fatal("a release build should look for a newer one")
	}
	out, _ := m.Update(cmd())
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "↑ v1.3.0") {
		t.Error("header lacks ↑ v1.3.0")
	}
	p := typePalette(t, m, "update")
	if got := paletteLabels(p); len(got) != 1 || !strings.Contains(got[0], "v1.3.0 is out  go install") {
		t.Errorf("palette = %q", got)
	}
	if _, cmd := p.handleKey(keyMsg(t, "enter")); cmd == nil {
		t.Error("enter should copy the install command")
	}
	m.checkRelease()()
	if n.Load() != 1 {
		t.Errorf("looked up %d times, want once a day", n.Load())
	}
}

func TestReleaseNoticeOff(t *testing.T) {
	n := fakeReleases(t, "v1.3.0")
	m := jiraTabModel(t).WithVersion("dev", "")
	if m.checkRelease() != nil {
		t.Error("a dev build should not look")
	}
	m = m.WithVersion("v1.2.0", "")
	m.opts.updateCheck = false
	if m.checkRelease() != nil {
		t.Error("update_check: off should not look")
	}
	m.opts.updateCheck = true
	out, _ := m.Update(releaseMsg{"v1.2.0"})
	if strings.Contains(out.(Model).View().Content, "↑") {
		t.Error("the same release is no news")
	}
	if n.Load() != 0 {
		t.Errorf("looked up %d times", n.Load())
	}
}
