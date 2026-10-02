package web

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// TestIcons: every icon('name') the frontend draws is in lib/icons.js, and every icon there is used.
func TestIcons(t *testing.T) {
	src, err := fs.ReadFile(staticFS, "static/js/lib/icons.js")
	if err != nil {
		t.Fatal(err)
	}
	table, rest, ok := strings.Cut(string(src), "\n};\n")
	if !ok {
		t.Fatal("icons.js: no P table")
	}
	have := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  '([a-z0-9-]+)': `).FindAllStringSubmatch(table, -1) {
		have[m[1]] = true
	}
	code := rest
	fs.WalkDir(staticFS, "static/js", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && path.Ext(p) == ".js" && p != "static/js/lib/icons.js" {
			b, _ := fs.ReadFile(staticFS, p)
			code += string(b)
		}
		return nil
	})
	for _, m := range regexp.MustCompile(`\b(?:icon\(|setIcon\([^,()]+, )'([a-z0-9-]+)'`).FindAllStringSubmatch(code, -1) {
		if !have[m[1]] {
			t.Errorf("icon %q is not in lib/icons.js", m[1])
		}
	}
	for name := range have {
		if !strings.Contains(code, "'"+name+"'") {
			t.Errorf("icon %q in lib/icons.js is never used", name)
		}
	}
}
