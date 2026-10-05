package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/store"
)

// Once a day the latest GitHub release is looked up; a newer one than the
// running build shows as ↑ v1.2 in the header and in the palette with the
// command that installs it. ui.update_check: off stops it; a dev build
// never looks.

// releaseAPI is laneway's latest release; releasePage its page. Tests
// point releaseAPI at a fake.
var releaseAPI = "https://api.github.com/repos/cornedor/laneway/releases/latest"

const releasePage = "https://github.com/cornedor/laneway/releases/latest"

// releaseMeta keeps the last look: "<unix>\t<tag>".
const releaseMeta = "release_latest"

// releaseMsg is the latest release's tag.
type releaseMsg struct{ tag string }

// WithVersion names the running build and the command that updates it
// ("" when there is none: the release page opens instead).
func (m Model) WithVersion(version, upgradeCmd string) Model {
	m.version, m.upgradeCmd = version, upgradeCmd
	return m
}

// WithDemo is m for laneway -demo: herdr stays off, so the demo neither
// shows nor drives the machine's real agents.
func (m Model) WithDemo() Model {
	m.herdr, m.demo = nil, true
	return m
}

// errOffInDemo: the demo runs nothing on this machine (no browser,
// configured command, LLM, clipboard or screenshot tool), as the web's
// demoGate.
var errOffInDemo = errors.New("not in the demo, which runs nothing on this machine")

// noHerdr says why there are no agents: none running, or the demo.
func (m *Model) noHerdr() string {
	if m.demo {
		return "herdr is off in the demo"
	}
	return "no herdr running"
}

// checkRelease reads the latest release, from the store when looked up in
// the last day. Failing quietly: offline is no news.
func (m *Model) checkRelease() tea.Cmd {
	if _, ok := parseSemver(m.version); !m.opts.updateCheck || !ok {
		return nil
	}
	st, ctx := m.store, m.ctx
	if tag, ok := keptRelease(st); ok {
		return func() tea.Msg { return releaseMsg{tag} }
	}
	return func() tea.Msg {
		tag, err := fetchRelease(ctx, st)
		if err != nil {
			return nil
		}
		return releaseMsg{tag}
	}
}

// NewerRelease is the latest release's tag when it is newer than version,
// "" otherwise: for laneway web, which shares the store's daily look. A dev
// build never looks, and offline is no news.
func NewerRelease(ctx context.Context, st *store.Store, version string) string {
	if _, ok := parseSemver(version); !ok {
		return ""
	}
	tag, ok := keptRelease(st)
	if !ok {
		var err error
		if tag, err = fetchRelease(ctx, st); err != nil {
			return ""
		}
	}
	if newerSemver(tag, version) {
		return tag
	}
	return ""
}

// ReleasePage is the latest release's page.
const ReleasePage = releasePage

// keptRelease is the tag looked up in the last day.
func keptRelease(st *store.Store) (string, bool) {
	if st == nil {
		return "", false
	}
	v, ok, _ := st.GetMeta(releaseMeta)
	if !ok {
		return "", false
	}
	at, tag, _ := strings.Cut(v, "\t")
	if sec, err := strconv.ParseInt(at, 10, 64); err == nil && time.Since(time.Unix(sec, 0)) < 24*time.Hour {
		return tag, true
	}
	return "", false
}

// fetchRelease asks GitHub and keeps the answer for a day.
func fetchRelease(ctx context.Context, st *store.Store) (string, error) {
	tag, err := latestRelease(ctx)
	if err == nil && st != nil {
		_ = st.SetMeta(releaseMeta, fmt.Sprintf("%d\t%s", time.Now().Unix(), tag))
	}
	return tag, err
}

func latestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("releases: %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

func (m *Model) handleRelease(msg releaseMsg) {
	if newerSemver(msg.tag, m.version) {
		m.newRelease = msg.tag
	}
}

// upgradeHint is what the palette's update row does.
func (m *Model) upgradeHint() string {
	if m.upgradeCmd != "" {
		return m.upgradeCmd
	}
	return "open the release page"
}

// upgrade copies the upgrade command, else opens the release page.
func (m *Model) upgrade() tea.Cmd {
	if m.upgradeCmd == "" {
		m.status = "opening " + releasePage + "…"
		return m.openOpenable(openable{name: "the release page", url: releasePage})
	}
	m.status = "copied " + m.upgradeCmd
	return tea.SetClipboard(m.upgradeCmd)
}

// semver is vMAJOR.MINOR.PATCH; pre is whether a pre-release part follows
// (a go install pseudo-version has one).
type semver struct {
	n   [3]int
	pre bool
}

// parseSemver reads [v]MAJOR.MINOR.PATCH[-pre][+build]: a release build's
// version has no v, its tag does.
func parseSemver(v string) (semver, bool) {
	var s semver
	rest := strings.TrimPrefix(v, "v")
	rest, _, _ = strings.Cut(rest, "+")
	rest, pre, hasPre := strings.Cut(rest, "-")
	s.pre = hasPre && pre != ""
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, false
		}
		s.n[i] = n
	}
	return s, true
}

// newerSemver is whether tag is a release newer than cur.
func newerSemver(tag, cur string) bool {
	t, ok := parseSemver(tag)
	c, ok2 := parseSemver(cur)
	if !ok || !ok2 || t.pre {
		return false
	}
	for i := range t.n {
		if t.n[i] != c.n[i] {
			return t.n[i] > c.n[i]
		}
	}
	return c.pre // v1.2.0 is newer than v1.2.0-rc1
}
