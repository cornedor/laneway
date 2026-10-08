package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// The work timer, kept where the TUI keeps its own (internal/ui/worklog.go:
// "KEY unix"), so T in either runs the one timer and `laneway prompt` shows it.

const timerMeta = "jira_tab:timer"

type workTimer struct {
	Key   string
	Start time.Time
}

func init() {
	get("/timer", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		v, ok, _ := s.opt.Store.GetMeta(timerMeta)
		key, unix, _ := strings.Cut(v, " ")
		sec, err := strconv.ParseInt(unix, 10, 64)
		if !ok || key == "" || err != nil {
			return workTimer{}, nil // Key "": none runs
		}
		return workTimer{key, time.Unix(sec, 0)}, nil
	})
	put("/timer", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[workTimer](r)
		if err != nil {
			return nil, err
		}
		if b.Key == "" {
			return nil, s.opt.Store.SetMeta(timerMeta, "")
		}
		if !jira.ValidKey(b.Key) || b.Start.IsZero() {
			return nil, badRequest(i18n.T("timer needs an issue key and a start"))
		}
		return nil, s.opt.Store.SetMeta(timerMeta, b.Key+" "+strconv.FormatInt(b.Start.Unix(), 10))
	})
}
