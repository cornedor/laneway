package web

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/cornedor/laneway/internal/i18n"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// avatars are kept in memory for the life of the server: people change
// their picture rarely and a board shows the same few faces all day.
var avatars = struct {
	sync.Mutex
	m map[string]avatarImg
}{m: map[string]avatarImg{}}

type avatarImg struct {
	body []byte
	ctyp string
}

const avatarCacheMax = 512

func init() {
	handle("GET /api/avatar/{id}", serveAvatar)
}

// serveAvatar proxies the avatar registered under {id} (see avatarID) with
// the Jira client's auth. The browser never names a URL: an unknown id is a
// 404, and the client still refuses any host but the instance and the fixed
// avatar prefixes.
func serveAvatar(s *Server, w http.ResponseWriter, r *http.Request) {
	u, ok := avatarURLs.lookup(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	avatars.Lock()
	img, ok := avatars.m[u]
	avatars.Unlock()
	if !ok {
		body, err := s.Client().Avatar(r.Context(), u)
		if err != nil {
			code := http.StatusBadGateway
			if strings.Contains(err.Error(), "bad url") || strings.Contains(err.Error(), "refused") {
				code = http.StatusBadRequest
			}
			http.Error(w, i18n.T("avatar unavailable"), code)
			return
		}
		img = avatarImg{body, avatarType(body)}
		if img.ctyp == "" {
			http.Error(w, i18n.T("not an image"), http.StatusUnsupportedMediaType)
			return
		}
		avatars.Lock()
		if len(avatars.m) >= avatarCacheMax {
			clear(avatars.m)
		}
		avatars.m[u] = img
		avatars.Unlock()
	}
	w.Header().Set("Content-Type", img.ctyp)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// An SVG avatar is an image only: no scripts if opened directly.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	_, _ = w.Write(img.body)
}

// avatarRegistry maps short ids to the avatar URLs Jira gave, least recently
// used out first, so the browser asks for /api/avatar/<id> and never supplies
// a URL itself.
type avatarRegistry struct {
	mu  sync.Mutex
	max int
	ll  *list.List // of avatarEntry, front = most recent
	by  map[string]*list.Element
}

type avatarEntry struct{ id, url string }

const avatarRegistryMax = 2048

var avatarURLs = &avatarRegistry{max: avatarRegistryMax, ll: list.New(), by: map[string]*list.Element{}}

// register remembers u and returns its id: the same URL, the same id.
func (g *avatarRegistry) register(u string) string {
	sum := sha256.Sum256([]byte(u))
	id := hex.EncodeToString(sum[:8])
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.by[id]; ok {
		g.ll.MoveToFront(e)
		return id
	}
	g.by[id] = g.ll.PushFront(avatarEntry{id, u})
	for g.ll.Len() > g.max {
		last := g.ll.Back()
		delete(g.by, last.Value.(avatarEntry).id)
		g.ll.Remove(last)
	}
	return id
}

func (g *avatarRegistry) lookup(id string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.by[id]
	if !ok {
		return "", false
	}
	g.ll.MoveToFront(e)
	return e.Value.(avatarEntry).url, true
}

// avatarField matches the JSON members that carry an avatar URL.
var avatarField = regexp.MustCompile(`"(AvatarURL|AuthorAvatar|TypeAvatar|Avatar|avatar)":("https?:(?:[^"\\]|\\.)*")`)

// rewriteAvatars swaps every avatar URL in an encoded response for
// /api/avatar/<id>, registering the original.
func rewriteAvatars(b []byte) []byte {
	if !bytes.Contains(b, []byte("vatar")) {
		return b
	}
	return avatarField.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := avatarField.FindSubmatch(m)
		var u string
		if json.Unmarshal(sub[2], &u) != nil {
			return m
		}
		return []byte(`"` + string(sub[1]) + `":"/api/avatar/` + avatarURLs.register(u) + `"`)
	})
}

// avatarType is the image type of body, "" when it is not one we serve.
func avatarType(body []byte) string {
	head := bytes.TrimSpace(body[:min(len(body), 512)])
	if bytes.HasPrefix(head, []byte("<svg")) || (bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(head, []byte("<svg"))) {
		return "image/svg+xml"
	}
	t := http.DetectContentType(body)
	if strings.HasPrefix(t, "image/") {
		return t
	}
	return ""
}
