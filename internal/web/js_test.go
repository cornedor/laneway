package web

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJS runs the frontend's pure-module tests (jstest/*.test.mjs) with node, when it is installed.
func TestJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	files, _ := filepath.Glob("jstest/*.test.mjs")
	if len(files) == 0 {
		t.Fatal("no jstest files")
	}
	out, err := exec.Command(node, append([]string{"--test"}, files...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestJSSyntax parses every frontend module, so a typo fails here and not
// only when its view is opened.
func TestJSSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	var files []string
	_ = filepath.WalkDir("static/js", func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".js") {
			files = append(files, p)
		}
		return err
	})
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, "--check", "--input-type=module")
		cmd.Stdin = bytes.NewReader(src)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: %s", f, out)
		}
	}
}
