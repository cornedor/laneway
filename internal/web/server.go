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
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
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
	Client  *jira.Client
	Jira    config.JiraConfig
	UI      config.UIConfig
	Store   *store.Store
	Site    string
	Sites   []string
	Demo    bool
	Version string
}

// Server is the HTTP handler: API under /api, assets everywhere else.
type Server struct {
	opt Options
	mux *http.ServeMux
	ctx context.Context
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
	s := &Server{opt: opt, mux: http.NewServeMux(), ctx: ctx}
	routesMu.Lock()
	for _, rt := range routes {
		h := rt.h
		s.mux.HandleFunc(rt.pattern, func(w http.ResponseWriter, r *http.Request) { h(s, w, r) })
	}
	routesMu.Unlock()
	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", assets(http.FS(sub)))
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

// assets serves the embedded frontend; files are revalidated each load (they
// change with the binary) but answer 304 from their ETag-less modtime.
func assets(root http.FileSystem) http.Handler {
	fsrv := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && compressible(r.URL.Path) {
			w.Header().Set("Vary", "Accept-Encoding")
			gz := gzipWriter(w)
			defer gz.Close()
			fsrv.ServeHTTP(gz, r)
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

type gzw struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func gzipWriter(w http.ResponseWriter) *gzw {
	w.Header().Set("Content-Encoding", "gzip")
	return &gzw{w, gzip.NewWriter(w)}
}
func (g *gzw) WriteHeader(c int) {
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(c)
}
func (g *gzw) Write(b []byte) (int, error) { g.Header().Del("Content-Length"); return g.gz.Write(b) }
func (g *gzw) Close() error                { return g.gz.Close() }

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
