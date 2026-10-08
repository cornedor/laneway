package i18n

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var verbRe = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z%]`)

func verbs(s string) []string {
	v := verbRe.FindAllString(s, -1)
	slices.Sort(v)
	return v
}

// A translation takes the same arguments as its English.
func TestVerbs(t *testing.T) {
	for _, lang := range Langs {
		for k, v := range Catalog(lang) {
			if k != "" && !slices.Equal(verbs(k), verbs(v)) {
				t.Errorf("%s: %q → %q: verbs %v, want %v", lang, k, v, verbs(v), verbs(k))
			}
		}
	}
}

// Two files of a language never translate a text differently.
func TestConflicts(t *testing.T) {
	for _, lang := range Langs {
		seen := map[string][2]string{} // key → file, value
		ents, _ := fs.ReadDir(files, lang)
		for _, e := range ents {
			b, _ := files.ReadFile(lang + "/" + e.Name())
			var part map[string]string
			if err := json.Unmarshal(b, &part); err != nil {
				t.Fatalf("%s/%s: %v", lang, e.Name(), err)
			}
			for k, v := range part {
				if p, ok := seen[k]; ok && p[1] != v {
					t.Errorf("%s: %q is %q in %s and %q in %s", lang, k, p[1], p[0], v, e.Name())
				}
				seen[k] = [2]string{e.Name(), v}
			}
		}
	}
}

// Every text the source marks (i18n.T/Tf/Tn/N with a literal in Go, T/Tn in
// the web's JS) has a translation, and every translation is used. The
// missing ones are printed as JSON to paste into a catalog.
func TestCoverage(t *testing.T) {
	used := sourceKeys(t)
	for _, lang := range Langs {
		cat := Catalog(lang)
		var missing []string
		for k := range used {
			if _, ok := cat[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			m := map[string]string{}
			for _, k := range missing {
				m[k] = ""
			}
			b, _ := json.MarshalIndent(m, "", "  ")
			t.Errorf("%s: %d texts without a translation (where: go test -run TestCoverage -v):\n%s", lang, len(missing), b)
			for _, k := range missing {
				t.Logf("%q: %s", k, strings.Join(used[k], ", "))
			}
		}
		for k := range cat {
			if _, ok := used[k]; !ok && k != "" {
				t.Errorf("%s: %q is translated but not used", lang, k)
			}
		}
	}
}

var (
	jsStr = `'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"`
	jsT   = regexp.MustCompile(`\bT\(\s*(` + jsStr + `)`)
	jsTn  = regexp.MustCompile(`\bTn\([^,()]*(?:\([^()]*\))?[^,()]*,\s*(` + jsStr + `)\s*,\s*(` + jsStr + `)`)
)

// sourceKeys are the marked texts of the repo's source, with where.
func sourceKeys(t *testing.T) map[string][]string {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string][]string{}
	add := func(k, where string) { keys[k] = append(keys[k], where) }
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" || rel == "internal/i18n") {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go"):
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := c.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "i18n" {
					return true
				}
				var args []ast.Expr
				switch sel.Sel.Name {
				case "T", "Tf", "N":
					args = c.Args[:min(1, len(c.Args))]
				case "Tn":
					if len(c.Args) >= 3 {
						args = c.Args[1:3]
					}
				}
				for _, a := range args {
					if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil {
							add(s, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
						}
					}
				}
				return true
			})
		case strings.HasSuffix(p, ".js") && strings.Contains(rel, "static/js/") && !strings.HasSuffix(rel, "lib/i18n.js"):
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			src := string(b)
			line := func(i int) string { return rel + ":" + strconv.Itoa(strings.Count(src[:i], "\n")+1) }
			for _, m := range jsT.FindAllStringSubmatchIndex(src, -1) {
				add(jsUnquote(src[m[2]:m[3]]), line(m[2]))
			}
			for _, m := range jsTn.FindAllStringSubmatchIndex(src, -1) {
				add(jsUnquote(src[m[2]:m[3]]), line(m[2]))
				add(jsUnquote(src[m[4]:m[5]]), line(m[4]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// jsUnquote reads a JS string literal ('…' or "…").
func jsUnquote(q string) string {
	body := q[1 : len(q)-1]
	if q[0] == '\'' {
		body = strings.ReplaceAll(strings.ReplaceAll(body, `\'`, `'`), `"`, `\"`)
	}
	s, err := strconv.Unquote(`"` + body + `"`)
	if err != nil {
		return body
	}
	return s
}

func TestT(t *testing.T) {
	t.Setenv("LANEWAY_LANG", "")
	defer SetLang("")
	m := map[string]string{"": "nl", "%d issues": "%d issues (nl)"}
	cur.Store(&m)
	if got := Tn(2, "%d issue", "%d issues", 2); got != "2 issues (nl)" {
		t.Errorf("Tn = %q", got)
	}
	if got := T("untranslated"); got != "untranslated" {
		t.Errorf("T = %q", got)
	}
	if Lang() != "nl" {
		t.Errorf("Lang = %q", Lang())
	}
	SetLang("nl_NL.UTF-8")
	if Lang() != "nl" {
		t.Errorf("Lang after SetLang(nl_NL.UTF-8) = %q", Lang())
	}
	SetLang("de")
	if Lang() != "en" {
		t.Errorf("Lang after SetLang(de) = %q", Lang())
	}
}
