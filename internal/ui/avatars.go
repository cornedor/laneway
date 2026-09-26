package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/cornedor/laneway/internal/jira"
)

// Avatar images on the assignee chips, where the terminal draws kitty
// graphics: each person's 48px avatar is fetched once (then read from the
// disk cache), sent to the terminal once a session, and drawn two cells
// wide where the initials were. Until then, and elsewhere, the initials.

// avatarCacheDir is where downloaded avatars are kept; tests swap it.
var avatarCacheDir = func() string {
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "laneway", "avatars")
}

// avatarPlace is each person's avatar placeholder once it is on the
// terminal, by display name; renders run on one goroutine.
var avatarPlace = map[string]string{}

// avatarLoadedMsg carries one avatar, encoded for transmit.
type avatarLoadedMsg struct {
	url string
	seq string
	err error
}

// fetchAvatars starts loading the avatars of the loaded cards' assignees
// not asked for yet this session.
func (m *Model) fetchAvatars() tea.Cmd {
	ii := m.images
	if ii == nil || !ii.on {
		return nil
	}
	var cmds []tea.Cmd
	for _, c := range m.jiraTab.cards {
		u := c.AvatarURL
		if u == "" || ii.avatars[u] != nil {
			continue
		}
		id := ii.nextID & 0xFFFFFF
		ii.nextID++
		ii.avatars[u] = &panelImage{state: imgLoading, id: id, cols: 2, rows: 1}
		cmds = append(cmds, loadAvatar(m.ctx, m.jiraClient, u, id))
	}
	return tea.Batch(cmds...)
}

func loadAvatar(ctx context.Context, c *jira.Client, u string, id uint32) tea.Cmd {
	return func() tea.Msg {
		b, err := cachedAvatar(ctx, c, u)
		if err != nil {
			return avatarLoadedMsg{url: u, err: err}
		}
		seq, err := encodeAvatar(id, b)
		return avatarLoadedMsg{url: u, seq: seq, err: err}
	}
}

// cachedAvatar is u's image from the disk cache, else downloaded and kept
// there.
func cachedAvatar(ctx context.Context, c *jira.Client, u string) ([]byte, error) {
	dir := avatarCacheDir()
	sum := sha256.Sum256([]byte(u))
	path := filepath.Join(dir, hex.EncodeToString(sum[:16]))
	if dir != "" {
		if b, err := os.ReadFile(path); err == nil {
			return b, nil
		}
	}
	b, err := c.Avatar(ctx, u)
	if err != nil {
		return nil, err
	}
	if dir != "" && os.MkdirAll(dir, 0o700) == nil {
		_ = os.WriteFile(path, b, 0o600) // a cache: failing to keep it costs a download
	}
	return b, nil
}

// encodeAvatar builds the transmit sequence for an avatar placed two cells
// wide and one high: square in a 1:2 cell.
func encodeAvatar(id uint32, b []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("decode avatar: %w", err)
	}
	var sb strings.Builder
	err = kitty.EncodeGraphics(&sb, shrinkImage(img, 96), &kitty.Options{
		Action:           kitty.TransmitAndPut,
		VirtualPlacement: true,
		ID:               int(id),
		Rows:             1,
		Columns:          2,
		Format:           kitty.PNG,
		Transmission:     kitty.Direct,
		Quite:            2,
		Chunk:            true,
	})
	return sb.String(), err
}

// handleAvatarLoaded sends a loaded avatar to the terminal and redraws the
// chips of everyone who has it.
func (m Model) handleAvatarLoaded(msg avatarLoadedMsg) (tea.Model, tea.Cmd) {
	e := m.images.avatars[msg.url]
	if e == nil {
		return m, nil
	}
	if msg.err != nil {
		e.state = imgFailed // the initials stay
		return m, nil
	}
	e.state = imgReady
	place := kittyPlaceholder(e.id, 1, 2)[0]
	for _, c := range m.jiraTab.cards {
		if c.AvatarURL == msg.url && c.Assignee != "" {
			avatarPlace[c.Assignee] = place
			delete(avatars, c.Assignee)
		}
	}
	m.jiraTab.rows = nil
	m.renderJira()
	return m, tea.Raw(m.images.wrap(msg.seq))
}

// imageRun is a kitty placeholder run: its id colour, the cells, the reset.
var imageRun = regexp.MustCompile("\x1b\\[38;2;\\d+;\\d+;\\d+m(?:\U0010EEEE\\p{Mn}*)+\x1b\\[39m")

// stripKeepImages is ansi.Strip that keeps image placeholders drawable:
// their colour is the image's id.
func stripKeepImages(s string) string {
	locs := imageRun.FindAllStringIndex(s, -1)
	if locs == nil {
		return ansi.Strip(s)
	}
	var b strings.Builder
	at := 0
	for _, l := range locs {
		b.WriteString(ansi.Strip(s[at:l[0]]))
		b.WriteString(s[l[0]:l[1]])
		at = l[1]
	}
	b.WriteString(ansi.Strip(s[at:]))
	return b.String()
}
