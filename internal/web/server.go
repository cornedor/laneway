// Package web serves laneway in a browser: a JSON API over internal/jira and
// an embedded, build-free frontend (static/).
//
// Every API file registers its routes from init() with handle/get/post, so
// feature areas live in their own api_*.go files.
package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

//go:embed static
var staticFS embed.FS

// Options configure a Server.
type Options struct {
	Client *jira.Client
	Jira   config.JiraConfig
	UI     config.UIConfig
	// ConfigPath is the config file the settings write to; empty: read-only.
	ConfigPath string
	Store      *store.Store
	Site       string
	Sites      []string
	Demo       bool
	Version    string
	// Open builds the options of another site (nil: no switching).
	Open func(site string) (Options, error)
}

// Server is the HTTP handler: API under /api, assets everywhere else.
type Server struct {
	opt   Options
	mux   *http.ServeMux
	ctx   context.Context
	sites *siteSet
}

// Client is the Jira client; handlers take it from the request's Server.
func (s *Server) Client() *jira.Client { return s.opt.Client }

// Options returns the server's options.
func (s *Server) Options() Options { return s.opt }

type route struct {
	pattern string
	h       func(s *Server, w http.ResponseWriter, r *http.Request)
}

var (
	routesMu sync.Mutex
	routes   []route
)

// handle registers a raw handler under a Go 1.22 mux pattern ("GET /api/x/{id}").
func handle(pattern string, h func(s *Server, w http.ResponseWriter, r *http.Request)) {
	routesMu.Lock()
	routes = append(routes, route{pattern, h})
	routesMu.Unlock()
}

// Handler is a JSON endpoint: the result is encoded, an error becomes a
// JSON {error} with a fitting status.
type Handler func(ctx context.Context, s *Server, r *http.Request) (any, error)

// get registers a GET endpoint at /api+path.
func get(path string, h Handler) { api("GET "+path, h) }

// post registers a POST endpoint at /api+path; the body is read with Body.
func post(path string, h Handler) { api("POST "+path, h) }

// put, del: likewise.
func put(path string, h Handler) { api("PUT "+path, h) }
func del(path string, h Handler) { api("DELETE "+path, h) }

func api(pattern string, h Handler) {
	method, path, _ := strings.Cut(pattern, " ")
	handle(method+" /api"+path, func(s *Server, w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		v, err := h(ctx, s, r)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, r, v)
	})
}

// Body decodes the request's JSON body into T.
func Body[T any](r *http.Request) (T, error) {
	var v T
	err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&v)
	if err != nil && !errors.Is(err, io.EOF) {
		return v, badRequest("bad JSON: " + err.Error())
	}
	return v, nil
}

// Q returns a query parameter.
func Q(r *http.Request, name string) string { return r.URL.Query().Get(name) }

type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

func badRequest(msg string) error { return httpError{http.StatusBadRequest, msg} }

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	var he httpError
	var re *jira.RequestError
	switch {
	case errors.As(err, &he):
		code = he.code
	case errors.Is(err, jira.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, context.DeadlineExceeded):
		code = http.StatusGatewayTimeout
	case errors.As(err, &re):
		code = http.StatusBadGateway
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// writeJSON encodes v, gzipped when the client takes it and the body is big.
func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	if v == nil {
		v = map[string]bool{"ok": true}
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if buf.Len() > 1024 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write(buf.Bytes())
		_ = gz.Close()
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// New builds the handler.
func New(ctx context.Context, opt Options) *Server {
	s := &Server{opt: opt, mux: http.NewServeMux(), ctx: ctx, sites: newSiteSet(opt)}
	routesMu.Lock()
	for _, rt := range routes {
		h := rt.h
		s.mux.HandleFunc(rt.pattern, func(w http.ResponseWriter, r *http.Request) { h(s.at(r), w, r) })
	}
	routesMu.Unlock()
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", assets(sub))
	return s
}

// ServeHTTP guards against other sites driving the API from the browser: a
// state-changing request must come from this origin.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	s.mux.ServeHTTP(w, r)
}

// assets serves the embedded frontend. embed.FS has no modtime, so each file
// gets an ETag from its content hash (computed once) and reloads answer 304.
func assets(root fs.FS) http.Handler {
	etags := map[string]string{}
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := fs.ReadFile(root, p); err == nil {
			sum := sha256.Sum256(b)
			etags["/"+p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return nil
	})
	etags["/"] = etags["/index.html"]
	types := map[string]string{".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".json": "application/json", ".webmanifest": "application/manifest+json", ".html": "text/html; charset=utf-8"}
	fsrv := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-cache")
		if t, ok := types[path.Ext(r.URL.Path)]; ok {
			h.Set("Content-Type", t)
		}
		gz := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && compressible(r.URL.Path)
		if e, ok := etags[r.URL.Path]; ok {
			if gz {
				e = e[:len(e)-1] + `-gz"`
			}
			h.Set("Etag", e)
		}
		if gz {
			h.Set("Vary", "Accept-Encoding")
			g := &gzw{ResponseWriter: w}
			defer g.Close()
			fsrv.ServeHTTP(g, r)
			return
		}
		fsrv.ServeHTTP(w, r)
	})
}

func compressible(p string) bool {
	for _, e := range []string{".js", ".css", ".html", ".svg", ".json", "/"} {
		if strings.HasSuffix(p, e) {
			return true
		}
	}
	return false
}

// gzw compresses 200 responses only; 304s and errors pass through untouched.
type gzw struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzw) WriteHeader(c int) {
	if c == http.StatusOK && g.Header().Get("Content-Encoding") == "" {
		g.Header().Del("Content-Length")
		g.Header().Set("Content-Encoding", "gzip")
		g.gz = gzip.NewWriter(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(c)
}
func (g *gzw) Write(b []byte) (int, error) {
	if g.gz == nil {
		return g.ResponseWriter.Write(b)
	}
	return g.gz.Write(b)
}
func (g *gzw) Close() error {
	if g.gz != nil {
		return g.gz.Close()
	}
	return nil
}

// Serve listens on addr and serves until ctx ends. A wildcard or public
// address is refused unless allowRemote: the API acts as you on Jira.
func Serve(ctx context.Context, addr string, allowRemote bool, h http.Handler, ready func(net.Addr)) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) && !allowRemote {
		return fmt.Errorf("%s is not a loopback address: the web UI acts as you on Jira; pass -remote to allow it", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if ready != nil {
		ready(ln.Addr())
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
