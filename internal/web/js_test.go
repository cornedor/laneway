package web

import (
	"os/exec"
	"path/filepath"
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
