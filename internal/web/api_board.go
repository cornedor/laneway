package web

import (
	"bytes"
	"net/http"
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
	handle("GET /api/avatar", serveAvatar)
}

// serveAvatar proxies ?u=<avatar url> with the Jira client's auth. The
// client refuses any host but the instance and Atlassian's avatar hosts.
func serveAvatar(s *Server, w http.ResponseWriter, r *http.Request) {
	u := Q(r, "u")
	if u == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
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
			http.Error(w, err.Error(), code)
			return
		}
		img = avatarImg{body, avatarType(body)}
		if img.ctyp == "" {
			http.Error(w, "not an image", http.StatusUnsupportedMediaType)
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
