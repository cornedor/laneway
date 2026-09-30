package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssetsETagAndTypes(t *testing.T) {
	sub, _ := fs.Sub(staticFS, "static")
	srv := httptest.NewServer(assets(sub))
	defer srv.Close()

	get := func(path, enc, inm string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Accept-Encoding", enc)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		res, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	for path, want := range map[string]string{"/js/app.js": "text/javascript; charset=utf-8", "/css/base.css": "text/css; charset=utf-8", "/": "text/html; charset=utf-8"} {
		res := get(path, "gzip", "")
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != want {
			t.Errorf("%s: %d %q", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
		if res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("Etag") == "" {
			t.Errorf("%s: encoding %q etag %q", path, res.Header.Get("Content-Encoding"), res.Header.Get("Etag"))
		}
		res2 := get(path, "gzip", res.Header.Get("Etag"))
		if res2.StatusCode != http.StatusNotModified || res2.Header.Get("Content-Encoding") != "" {
			t.Errorf("%s: revalidate got %d enc %q", path, res2.StatusCode, res2.Header.Get("Content-Encoding"))
		}
		plain := get(path, "", "")
		if plain.Header.Get("Content-Encoding") != "" || plain.Header.Get("Etag") == res.Header.Get("Etag") {
			t.Errorf("%s: plain and gzip variants share headers", path)
		}
	}
}
