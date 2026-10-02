package gitlab

import (
	"strings"
	"sync"

	"github.com/cornedor/laneway/internal/forge"
)

// Sites are the instances merge request links resolve to: the configured
// ones, and any other host glab is logged in to. Safe for concurrent use.
type Sites struct {
	mu      sync.Mutex
	clients map[string]*Client // by host; nil for a host without a token
	// glab is TokenFromGlab; tests swap it.
	glab func(host string) string
}

// NewSites is the configured instances by host. One without a token takes
// glab's for its host, asked when a link first needs it.
func NewSites(cfgs []Config) *Sites {
	s := &Sites{clients: map[string]*Client{}, glab: TokenFromGlab}
	for _, cfg := range cfgs {
		if h := forge.HostOf(forge.NormalizeBaseURL(cfg.BaseURL)); h != "" {
			s.clients[h] = New(cfg)
		}
	}
	return s
}

// For is the client for the instance link points at, nil without a token
// for it.
func (s *Sites) For(link string) *Client {
	h := forge.HostOf(link)
	if h == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[h]
	if ok && (c == nil || c.Enabled()) {
		return c
	}
	cfg := Config{BaseURL: "https://" + h}
	if c != nil {
		cfg.BaseURL = c.BaseURL()
	}
	if tok := strings.TrimSpace(s.glab(h)); tok != "" {
		cfg.Token = tok
		c = New(cfg)
	} else {
		c = nil
	}
	s.clients[h] = c
	return c
}
