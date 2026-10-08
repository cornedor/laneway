package web

import (
	"bytes"
	"context"
	"github.com/cornedor/laneway/internal/i18n"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Uploaded fonts (Settings > Appearance): files in <state dir>/fonts, listed at
// /api/fonts and served from /fonts/custom/<name>. Small, sniffed, sanitised.

const maxFontBytes = 4 << 20

var (
	fontName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	fontExts = map[string]string{".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf"}
)

type fontFile struct {
	Name string
	Size int64
}

func fontsDir(o Options) string {
	if o.Store == nil || o.Store.Path() == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(o.Store.Path()), "fonts")
}

// cleanFontName keeps letters, digits, dot, dash and underscore; anything else becomes "-".
func cleanFontName(n string) string {
	n = strings.ToLower(filepath.Base(strings.ReplaceAll(n, "\\", "/")))
	n = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(n, "-")
	n = strings.TrimLeft(n, ".-")
	ext := filepath.Ext(n)
	if len(n) > 64 {
		n = n[:64-len(ext)] + ext
	}
	return n
}

func fontMagic(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	h := b[:4]
	return bytes.Equal(h, []byte("wOF2")) || bytes.Equal(h, []byte("wOFF")) || bytes.Equal(h, []byte{0, 1, 0, 0}) ||
		bytes.Equal(h, []byte("OTTO")) || bytes.Equal(h, []byte("true"))
}

func listFonts(o Options) []fontFile {
	out := []fontFile{}
	d := fontsDir(o)
	if d == "" {
		return out
	}
	es, _ := os.ReadDir(d)
	for _, e := range es {
		if fi, err := e.Info(); err == nil && !e.IsDir() && fontName.MatchString(e.Name()) && fontExts[strings.ToLower(filepath.Ext(e.Name()))] != "" {
			out = append(out, fontFile{e.Name(), fi.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func init() {
	api("GET /fonts", func(ctx context.Context, s *Server, r *http.Request) (any, error) { return listFonts(s.opt), nil })
	// The body is the raw file; the name comes from ?name=.
	api("POST /fonts", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		d := fontsDir(s.opt)
		if d == "" {
			return nil, badRequest(i18n.T("no state directory to keep fonts in"))
		}
		name := cleanFontName(Q(r, "name"))
		if !fontName.MatchString(name) || fontExts[strings.ToLower(filepath.Ext(name))] == "" {
			return nil, badRequest(i18n.T("the file must be .woff2, .woff, .ttf or .otf"))
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, maxFontBytes+1))
		if err != nil {
			return nil, err
		}
		if len(b) > maxFontBytes {
			return nil, badRequest(i18n.T("font too large (max 4 MB)"))
		}
		if !fontMagic(b) {
			return nil, badRequest(i18n.T("not a font file"))
		}
		if len(listFonts(s.opt)) >= 20 {
			return nil, badRequest(i18n.T("20 fonts are enough; remove one first"))
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(d, name), b, 0o600); err != nil {
			return nil, err
		}
		return listFonts(s.opt), nil
	})
	api("DELETE /fonts/{name}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		d, name := fontsDir(s.opt), r.PathValue("name")
		if d == "" || !fontName.MatchString(name) {
			return nil, badRequest(i18n.T("bad name"))
		}
		if err := os.Remove(filepath.Join(d, name)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return listFonts(s.opt), nil
	})
	handle("GET /fonts/custom/{name}", func(s *Server, w http.ResponseWriter, r *http.Request) {
		d, name := fontsDir(s.opt), r.PathValue("name")
		ct := fontExts[strings.ToLower(filepath.Ext(name))]
		if d == "" || !fontName.MatchString(name) || ct == "" {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(d, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		fi, _ := f.Stat()
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, name, fi.ModTime(), f)
	})
}
