package web

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestServiceWorker(t *testing.T) {
	ts := searchDemo(t)
	res, err := http.Get(ts.URL + "/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	got := string(b)
	if strings.Contains(got, "__") || !strings.Contains(got, `"/js/app.js"`) || !strings.Contains(got, `"/manifest.webmanifest"`) {
		t.Errorf("sw.js not filled in:\n%.300s", got)
	}
	m, _ := http.Get(ts.URL + "/manifest.webmanifest")
	if ct := m.Header.Get("Content-Type"); !strings.Contains(ct, "manifest+json") {
		t.Errorf("manifest type %q", ct)
	}
}
