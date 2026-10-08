package web

import (
	"encoding/json"
	"net/http"

	"github.com/cornedor/laneway/internal/i18n"
)

// /api/i18n.js is the catalog of ui.language as a plain script, loaded
// before the modules so lib/i18n.js has it at their top level. Under /api
// so the service worker never keeps an old one.
func init() {
	handle("GET /api/i18n.js", func(s *Server, w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(i18n.Catalog(i18n.Lang()))
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte("window.LANEWAY_I18N=" + string(b) + ";document.documentElement.lang=" + `"` + i18n.Lang() + `"` + ";\n"))
	})
}
