package web

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFontUpload(t *testing.T) {
	ts, opt := toolsServer(t, nil)
	woff2 := append([]byte("wOF2"), make([]byte, 64)...)
	post := func(name string, body []byte) int {
		res, err := http.Post(ts.URL+"/api/fonts?name="+name, "application/octet-stream", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := post("My%20Font!.WOFF2", woff2); c != 200 {
		t.Fatalf("upload = %d", c)
	}
	if _, err := os.Stat(filepath.Join(fontsDir(opt), "my-font-.woff2")); err != nil {
		t.Fatalf("sanitised name not stored: %v", err)
	}
	if c := post("x.woff2", []byte("<html>not a font at all</html>")); c != 400 {
		t.Errorf("non-font = %d", c)
	}
	if c := post("x.exe", woff2); c != 400 {
		t.Errorf("bad extension = %d", c)
	}
	if c := post("..%2F..%2Fevil.ttf", append([]byte{0, 1, 0, 0}, make([]byte, 64)...)); c != 200 {
		t.Errorf("traversal name should be flattened, got %d", c)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fontsDir(opt)), "evil.ttf")); err == nil {
		t.Error("escaped the fonts dir")
	}
	if c := post("big.woff2", append([]byte("wOF2"), make([]byte, maxFontBytes)...)); c != 400 {
		t.Errorf("oversize = %d", c)
	}
	var list []fontFile
	if workCall(t, "GET", ts.URL+"/api/fonts", "", &list) != 200 || len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	res, err := http.Get(ts.URL + "/fonts/custom/my-font-.woff2")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "font/woff2" || !bytes.Equal(b, woff2) {
		t.Errorf("serve = %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res, _ := http.Get(ts.URL + "/fonts/custom/..%2Fstate.json"); res.StatusCode == 200 {
		t.Error("served outside the fonts dir")
	}
	if workCall(t, "DELETE", ts.URL+"/api/fonts/my-font-.woff2", "", &list) != 200 || len(list) != 1 || strings.Contains(list[0].Name, "my-font") {
		t.Errorf("delete = %+v", list)
	}
}
