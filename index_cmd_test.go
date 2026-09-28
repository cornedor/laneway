package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
)

func TestIndexCmd(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path, err := index.Path("work")
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ix.PutCards([]jira.Card{{Key: "ABC-1"}, {Key: "ABC-2"}, {Key: "XY-1"}})
	ix.Close()
	var out, errOut bytes.Buffer
	if code := subcommand([]string{"index"}, "", "work", &out, &errOut); code != 0 || !strings.Contains(out.String(), "ABC\t2\nXY\t1\n") {
		t.Fatalf("index = %d, %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := subcommand([]string{"index", "clear"}, "", "work", &out, &errOut); code != 0 {
		t.Fatalf("index clear = %d, %q", code, errOut.String())
	}
	out.Reset()
	if code := subcommand([]string{"index"}, "", "work", &out, &errOut); code != 0 || !strings.Contains(out.String(), "(empty)") {
		t.Errorf("after clear: %d, %q", code, out.String())
	}
	if code := subcommand([]string{"index", "bogus"}, "", "work", &out, &errOut); code != 2 {
		t.Errorf("index bogus = %d, want 2", code)
	}
}
