package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"sync"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// The site of a request is the lw_site cookie ("s:" + name, the name of
// the jira: block being ""), else the site the server started on. Each
// request gets a Server copy whose options are that site's, so handlers
// need no change: s.Client(), s.opt.Store and s.opt.Site follow the cookie.
const siteCookie = "lw_site"

type siteSet struct {
	mu   sync.Mutex
	base Options // the site the server started on
	open map[string]Options
}

func newSiteSet(base Options) *siteSet { return &siteSet{base: base, open: map[string]Options{}} }

// at is s for the site r asks for.
func (s *Server) at(r *http.Request) *Server {
	c, err := r.Cookie(siteCookie)
	if err != nil || len(c.Value) < 2 || c.Value[:2] != "s:" {
		return s
	}
	name, _ := url.QueryUnescape(c.Value[2:])
	o, err := s.siteOptions(name)
	if err != nil {
		return s
	}
	cp := *s
	cp.opt = o
	return &cp
}

// ClientFor is the Jira client for r's site.
func (s *Server) ClientFor(r *http.Request) *jira.Client { return s.at(r).opt.Client }

// siteOptions opens a site once and keeps it.
func (s *Server) siteOptions(name string) (Options, error) {
	base := s.sites.base
	if name == base.Site {
		return base, nil
	}
	if base.Open == nil || !slices.Contains(base.Sites, name) {
		return base, httpError{http.StatusNotFound, "unknown site"}
	}
	s.sites.mu.Lock()
	defer s.sites.mu.Unlock()
	if o, ok := s.sites.open[name]; ok {
		return o, nil
	}
	o, err := base.Open(name)
	if err != nil {
		return base, err
	}
	o.Open, o.Sites, o.Version, o.Demo = base.Open, base.Sites, base.Version, base.Demo
	s.sites.open[name] = o
	return o, nil
}

func init() {
	post("/site", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Site string }](r)
		if err != nil {
			return nil, err
		}
		if _, err := s.siteOptions(b.Site); err != nil {
			return nil, err
		}
		if !s.opt.Demo {
			_ = config.SetLastSite(b.Site)
		}
		return map[string]any{"site": b.Site, "cookie": "s:" + b.Site}, nil
	})
}
