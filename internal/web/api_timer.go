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
// "KEY unix [id]"), so T in either runs the one timer and `laneway prompt`
// shows it. With a timesheet that runs the timer itself (Caps.ServerTimer)
// the timer is its running entry, and the state file mirrors it.

const timerMeta = "jira_tab:timer"

type workTimer struct {
	Key   string
	Start time.Time
	// ID is the timesheet's running entry, "" for laneway's own timer.
	ID string `json:",omitempty"`
}

func (t workTimer) meta() string {
	if t.Key == "" {
		return ""
	}
	v := t.Key + " " + strconv.FormatInt(t.Start.Unix(), 10)
	if t.ID != "" {
		v += " " + t.ID
	}
	return v
}

func parseTimer(v string) workTimer {
	key, rest, _ := strings.Cut(v, " ")
	unix, id, _ := strings.Cut(rest, " ")
	sec, err := strconv.ParseInt(unix, 10, 64)
	if key == "" || err != nil {
		return workTimer{} // Key "": none runs
	}
	return workTimer{key, time.Unix(sec, 0), id}
}

// serverTimer is the timesheet's running entry as the timer, mirrored into
// the state file; ok is false when the timesheet has no timer.
func serverTimer(ctx context.Context, s *Server) (t workTimer, ok bool, err error) {
	if s.opt.Timesheet == nil {
		return t, false, nil
	}
	sh := s.Sheet()
	run, err := sh.Running(ctx)
	if err != nil || !sh.Caps().ServerTimer {
		return t, false, err
	}
	if run != nil && run.Key != "" { // an entry without an issue is no timer of laneway's
		t = workTimer{run.Key, time.Unix(run.Started.Unix(), 0), run.ID}
	}
	return t, true, s.opt.Store.SetMeta(timerMeta, t.meta())
}

// timerBody is PUT /timer's: Key and Start start or move the timer, Key ""
// clears it. Drop, with Key "", is the timer discarded: on the timesheet's
// timer its running entry on that issue is deleted (a clear after logging
// leaves it, the log stopped it already).
type timerBody struct {
	Key   string
	Start time.Time
	Drop  string
}

func init() {
	get("/timer", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		if t, ok, err := serverTimer(ctx, s); ok || err != nil {
			return t, err
		}
		v, _, _ := s.opt.Store.GetMeta(timerMeta)
		return parseTimer(v), nil
	})
	put("/timer", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[timerBody](r)
		if err != nil {
			return nil, err
		}
		if b.Key != "" && (!jira.ValidKey(b.Key) || b.Start.IsZero()) {
			return nil, badRequest(i18n.T("timer needs an issue key and a start"))
		}
		run, ok, err := serverTimer(ctx, s)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, s.opt.Store.SetMeta(timerMeta, workTimer{Key: b.Key, Start: b.Start}.meta())
		}
		sh := s.Sheet()
		switch {
		case b.Key == "":
			if b.Drop != "" && run.Key == b.Drop {
				err = sh.Delete(ctx, run.Key, []string{run.ID})
			}
		case b.Key == run.Key:
		case run.Key != "":
			err = sh.Move(ctx, run.Key, []string{run.ID}, b.Key)
		default:
			err = sh.Start(ctx, b.Key, "", b.Start)
		}
		if err != nil {
			return nil, err
		}
		_, _, err = serverTimer(ctx, s)
		return nil, err
	})
}
