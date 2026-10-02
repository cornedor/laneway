package gitlab

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/cornedor/laneway/internal/forge"
)

// Sites are the instances merge request links resolve to: the configured
// ones, and any other host glab is logged in to. Safe for concurrent use.
type Sites struct {
	mu      sync.Mutex
	clients map[string]*Client // by host; nil for a host without a token
	// from is where each host's token came from: "config" or "glab".
	from       map[string]string
	configured []string // the configured hosts, in the config's order
	// glab is TokenFromGlab and glabHosts GlabHosts; tests swap them.
	glab      func(host string) string
	glabHosts func() []string
}

// NewSites is the configured instances by host. One without a token takes
// glab's for its host, asked when a link first needs it.
func NewSites(cfgs []Config) *Sites {
	s := &Sites{clients: map[string]*Client{}, from: map[string]string{}, glab: TokenFromGlab, glabHosts: GlabHosts}
	for _, cfg := range cfgs {
		if h := forge.HostOf(forge.NormalizeBaseURL(cfg.BaseURL)); h != "" {
			s.clients[h] = New(cfg)
			s.configured = append(s.configured, h)
			if s.clients[h].Enabled() {
				s.from[h] = "config"
			}
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
		s.from[h] = "glab"
	} else {
		c = nil
	}
	s.clients[h] = c
	return c
}

// Status is one instance as the settings screens show it.
type Status struct {
	Host    string
	BaseURL string
	// From is where the token came from: "config", "glab", "" for none.
	From string
	// User is the token's account once checked; Err why that failed.
	User User
	Err  error
}

// Check signs in to every instance: the configured ones, then the hosts
// glab is logged in to.
func (s *Sites) Check(ctx context.Context) []Status {
	hosts := slices.Clone(s.configured)
	for _, h := range s.glabHosts() {
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	out := make([]Status, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		c := s.For("https://" + h)
		out[i] = Status{Host: h, BaseURL: "https://" + h}
		if c == nil {
			out[i].Err = forge.ErrNotConfigured
			continue
		}
		s.mu.Lock()
		out[i].BaseURL, out[i].From = c.BaseURL(), s.from[h]
		s.mu.Unlock()
		wg.Go(func() { out[i].User, out[i].Err = c.Me(ctx) })
	}
	wg.Wait()
	return out
}

// Summary is the status in a line, for both settings screens.
func (st Status) Summary() string {
	switch {
	case errors.Is(st.Err, forge.ErrNotConfigured):
		return "no token: add one under gitlab: in the config, or glab auth login --hostname " + st.Host
	case st.Err != nil:
		return st.Err.Error()
	}
	who := st.User.Username
	if st.User.Name != "" {
		who = st.User.Name + " (" + st.User.Username + ")"
	}
	return "signed in as " + who + ", token from " + st.From
}
