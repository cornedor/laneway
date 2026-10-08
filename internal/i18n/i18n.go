// Package i18n translates the text laneway shows. The English text is the
// key: T("Open issue") returns it in the current language, or as given when
// the catalog has none. Catalogs are nl/*.json (one per area, merged), a
// flat {"English": "translation"} map; the web frontend gets the same one
// from /api/i18n.js (lib/i18n.js).
//
// The language is ui.language (config.Load sets it), LANEWAY_LANG over it;
// English by default.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync/atomic"
)

//go:embed nl
var files embed.FS

// Langs are the languages with a catalog, besides English.
var Langs = []string{"nl"}

var cur atomic.Pointer[map[string]string]

func init() { SetLang("") }

// SetLang switches to lang ("nl", "nl_NL.UTF-8", "" or "en" for English);
// LANEWAY_LANG wins over it.
func SetLang(lang string) {
	if env := os.Getenv("LANEWAY_LANG"); env != "" {
		lang = env
	}
	m := Catalog(lang)
	cur.Store(&m)
}

// Lang is the language in use: "en" or one of Langs.
func Lang() string {
	m := *cur.Load()
	if l := m[""]; l != "" {
		return l
	}
	return "en"
}

// Catalog is lang's translations, nil for English or an unknown language.
// Key "" holds the language's name.
func Catalog(lang string) map[string]string {
	lang = norm(lang)
	ok := false
	for _, l := range Langs {
		ok = ok || l == lang
	}
	if !ok {
		return nil
	}
	m := map[string]string{"": lang}
	ents, _ := fs.ReadDir(files, lang)
	for _, e := range ents {
		if path.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := files.ReadFile(lang + "/" + e.Name())
		if err != nil {
			continue
		}
		var part map[string]string
		if err := json.Unmarshal(b, &part); err != nil {
			panic(fmt.Sprintf("i18n: %s/%s: %v", lang, e.Name(), err))
		}
		for k, v := range part {
			if k != "" && v != "" {
				m[k] = v
			}
		}
	}
	return m
}

// norm turns "nl_NL.UTF-8" or "nl-BE" into "nl".
func norm(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(lang, "_-.@"); i >= 0 {
		lang = lang[:i]
	}
	return lang
}

// T is msg in the current language.
func T(msg string) string {
	if s, ok := (*cur.Load())[msg]; ok {
		return s
	}
	return msg
}

// Tf is fmt.Sprintf with the format translated.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// Tn picks one (n == 1) or other, translated, and formats it with args:
// Tn(n, "%d issue", "%d issues", n).
func Tn(n int, one, other string, args ...any) string {
	f := other
	if n == 1 {
		f = one
	}
	if len(args) == 0 {
		return T(f)
	}
	return fmt.Sprintf(T(f), args...)
}

// N marks msg for translation without translating it, for text defined
// before the language is known (package-level tables); translate it where
// it is shown with T.
func N(msg string) string { return msg }
