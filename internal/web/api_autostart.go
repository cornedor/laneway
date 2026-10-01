package web

import (
	"context"
	"net/http"

	"github.com/cornedor/laneway/internal/autostart"
)

// Autostart runs laneway web at login: Argv with PATH set to Path, installed
// with Sys. Options.Autostart nil: not offered (the demo, -remote).
type Autostart struct {
	Sys  autostart.System
	Argv []string
	Path string
}

// autostartInfo is what the session tells the setup page and settings; nil
// when this system can't.
func (s *Server) autostartInfo() map[string]any {
	a := s.opt.Autostart
	if a == nil || !a.Sys.Supported() {
		return nil
	}
	return map[string]any{"enabled": a.Sys.Enabled(), "path": a.Sys.Path()}
}

func (s *Server) setAutostart(on bool) error {
	a := s.opt.Autostart
	if a == nil || !a.Sys.Supported() {
		return httpError{http.StatusNotImplemented, autostart.ErrUnsupported.Error()}
	}
	if on {
		return a.Sys.Enable(a.Argv, a.Path)
	}
	return a.Sys.Disable()
}

func init() {
	get("/autostart", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.autostartInfo(), nil
	})
	put("/autostart", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ On bool }](r)
		if err != nil {
			return nil, err
		}
		if err := s.setAutostart(b.On); err != nil {
			return nil, err
		}
		return s.autostartInfo(), nil
	})
}
