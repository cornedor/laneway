package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cornedor/laneway/internal/jira"
)

// Private notes per issue: plain files beside the state file, where the TUI
// keeps them (notes/ABC-12.md, notes-work/ for state-work.json), so both
// front ends see the same.

func notesDir(o Options) string {
	if o.Store == nil || o.Store.Path() == "" {
		return ""
	}
	p := o.Store.Path()
	base := strings.TrimSuffix(filepath.Base(p), ".json")
	return filepath.Join(filepath.Dir(p), "notes"+strings.TrimPrefix(base, "state"))
}

func notesPath(o Options, key string) string {
	d := notesDir(o)
	if d == "" || !jira.ValidKey(key) {
		return ""
	}
	return filepath.Join(d, key+".md")
}

func init() {
	get("/issues/{key}/notes", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		p := notesPath(s.opt, r.PathValue("key"))
		if p == "" {
			return nil, badRequest("no notes here")
		}
		raw, _ := os.ReadFile(p)
		return map[string]string{"Text": strings.TrimSpace(string(raw))}, nil
	})
	put("/issues/{key}/notes", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		p := notesPath(s.opt, r.PathValue("key"))
		if p == "" {
			return nil, badRequest("no notes here")
		}
		b, err := Body[struct{ Text string }](r)
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(b.Text)
		if text == "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			return nil, nil
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(p, []byte(text+"\n"), 0o600)
	})
	// Keys with notes, for marking them in lists.
	get("/notes", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		keys := []string{}
		if d := notesDir(s.opt); d != "" {
			entries, _ := os.ReadDir(d)
			for _, e := range entries {
				if k, ok := strings.CutSuffix(e.Name(), ".md"); ok && !e.IsDir() {
					keys = append(keys, strings.ToUpper(k))
				}
			}
		}
		return keys, nil
	})
}
