package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/i18n"
)

// The browser terminal: GET /api/agents/{pane}/terminal upgrades to a
// WebSocket and runs `herdr agent attach <pane>` on a pty for it, as the
// TUI's agent panel does (internal/ui/agent_panel.go). This is shell access
// to the agent's terminal, so it is for herdr agent panes only, and the
// upgrade passes ServeHTTP (Host, Origin, Sec-Fetch-Site, launch token) and
// must carry a same-origin Origin. Demo mode refuses it (demoGate).
//
// Frames: binary both ways is terminal bytes (output, keys and pastes);
// text from the browser is {"type":"resize","cols":n,"rows":n}, text from
// the server {"type":"exit","status":"…"} before it closes. Closing the
// socket hangs up the attach process; the agent keeps running.
//
// One browser terminal per pane: a new one takes over and the old closes
// with termTakenOver. herdr allows one direct attach per terminal (another,
// the TUI's panel say, makes ours exit with "already has an attached
// client"); ?takeover=1 passes --takeover to take it. The pane takes the
// attach's size.

const (
	termMaxMsg      = 64 << 10 // largest frame from the browser (it chunks pastes)
	termMaxSessions = 16
	termWriteWait   = 10 * time.Second

	// Close codes the browser acts on (4000-4999 is the application range).
	termTakenOver = 4001 // another window opened this pane: no reconnect
	termExited    = 4002 // the attach process ended
	termIdleClose = 4003 // nothing either way for termIdle: no reconnect
	termLifeClose = 4004 // termMaxLife reached: reconnecting re-checks access
)

// Limits, variables for the tests.
var (
	termIdle    = 30 * time.Minute
	termMaxLife = 8 * time.Hour
	termTick    = 30 * time.Second // keepalive ping and limit checks
)

var (
	termMu    sync.Mutex
	termPanes = map[string]*termSession{} // by socket path + pane
	termCount = 0
)

type termSession struct{ stop context.CancelCauseFunc }

// termEnd ends a session with a close code.
type termEnd struct {
	code   websocket.StatusCode
	reason string
}

func (e termEnd) Error() string { return e.reason }

var errTakenOver = termEnd{termTakenOver, "opened in another window"}

func init() {
	handle("GET /api/agents/{pane}/terminal", agentTerminal)
}

func agentTerminal(s *Server, w http.ResponseWriter, r *http.Request) {
	// ServeHTTP refuses a foreign Origin; a WebSocket from a page always
	// sends one, so its absence is refused too.
	o, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || o.Host == "" || o.Host != r.Host || o.Scheme != "http" && o.Scheme != "https" {
		http.Error(w, i18n.T("the terminal needs a same-origin Origin"), http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	c, err := needHerdr()
	var a herdr.Agent
	if err == nil {
		a, err = paneAgent(ctx, c, r)
	}
	cancel()
	if err != nil {
		writeErr(w, err)
		return
	}
	bin, err := exec.LookPath(herdrBin)
	if err != nil {
		writeErr(w, httpError{http.StatusNotImplemented, i18n.T("no herdr on PATH to attach to the agent")})
		return
	}
	cols, rows := termDim(Q(r, "cols"), 80, 10, 500), termDim(Q(r, "rows"), 24, 3, 300)

	sctx, stop := context.WithCancelCause(s.ctx)
	defer stop(nil)
	me := &termSession{stop}
	key := c.Path() + "\x00" + a.PaneID
	termMu.Lock()
	if termCount >= termMaxSessions {
		termMu.Unlock()
		writeErr(w, httpError{http.StatusServiceUnavailable, i18n.T("too many terminals open")})
		return
	}
	if old := termPanes[key]; old != nil {
		old.stop(errTakenOver)
	}
	termPanes[key] = me
	termCount++
	termMu.Unlock()
	defer func() {
		termMu.Lock()
		termCount--
		if termPanes[key] == me {
			delete(termPanes, key)
		}
		termMu.Unlock()
	}()

	ws, err := websocket.Accept(w, r, nil) // checks Origin against Host as well
	if err != nil {
		return
	}
	ws.SetReadLimit(termMaxMsg)

	args := []string{"agent", "attach", a.PaneID}
	if Q(r, "takeover") == "1" {
		args = append(args, "--takeover")
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(termEnv(), "HERDR_SOCKET_PATH="+c.Path())
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		ws.Close(websocket.StatusInternalError, trimReason("attach: "+err.Error()))
		return
	}
	runTerminal(sctx, stop, ws, f, cmd)
}

// runTerminal bridges ws and the pty until either side ends, then hangs up
// the attach process and closes the socket with the reason. Reads and
// writes use their own contexts: a cancelled one would drop the connection
// before the close frame says why.
func runTerminal(ctx context.Context, stop context.CancelCauseFunc, ws *websocket.Conn, f *os.File, cmd *exec.Cmd) {
	var mu sync.Mutex
	last := time.Now()
	touch := func() { mu.Lock(); last = time.Now(); mu.Unlock() }
	idleFor := func() time.Duration { mu.Lock(); defer mu.Unlock(); return time.Since(last) }

	exited := make(chan error, 1)
	go func() { // pty → browser
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 && ctx.Err() == nil {
				touch()
				wctx, cancel := context.WithTimeout(context.Background(), termWriteWait)
				if werr := ws.Write(wctx, websocket.MessageBinary, buf[:n]); werr != nil {
					stop(werr)
				}
				cancel()
			}
			if err != nil {
				break
			}
		}
		exited <- cmd.Wait()
		stop(termEnd{termExited, "exited"})
	}()
	go func() { // browser → pty
		for {
			typ, b, err := ws.Read(context.Background())
			if err != nil {
				stop(err)
				return
			}
			if ctx.Err() != nil {
				continue // closing: drain until the close handshake ends
			}
			touch()
			if typ == websocket.MessageBinary {
				if _, err := f.Write(b); err != nil {
					stop(err)
				}
				continue
			}
			var m struct {
				Type       string
				Cols, Rows int
			}
			if json.Unmarshal(b, &m) == nil && m.Type == "resize" {
				_ = pty.Setsize(f, &pty.Winsize{Cols: uint16(clamp(m.Cols, 10, 500)), Rows: uint16(clamp(m.Rows, 3, 300))})
			}
		}
	}()

	born := time.Now()
	tick := time.NewTicker(termTick)
	defer tick.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-tick.C:
			switch {
			case idleFor() >= termIdle:
				stop(termEnd{termIdleClose, "closed after " + termIdle.String() + " without activity"})
			case time.Since(born) >= termMaxLife:
				stop(termEnd{termLifeClose, "closed after " + termMaxLife.String() + ": reconnect"})
			default:
				pctx, cancel := context.WithTimeout(context.Background(), termWriteWait)
				if err := ws.Ping(pctx); err != nil {
					stop(err)
				}
				cancel()
			}
		}
	}

	// Hang up as a closed terminal would; the agent in herdr keeps running.
	hangupTerm(cmd.Process)
	var werr error
	select {
	case werr = <-exited:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case werr = <-exited:
		case <-time.After(2 * time.Second): // a child still holds the pty
			_ = f.Close()
			werr = <-exited
		}
	}
	_ = f.Close()

	var end termEnd
	switch cause := context.Cause(ctx); {
	case errors.As(cause, &end) && end.code == termExited:
		status := "exited"
		if werr != nil {
			status = "exited: " + werr.Error()
		}
		b, _ := json.Marshal(map[string]string{"type": "exit", "status": status})
		wctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = ws.Write(wctx, websocket.MessageText, b)
		cancel()
		ws.Close(termExited, trimReason(status))
	case errors.As(cause, &end):
		ws.Close(end.code, trimReason(end.reason))
	case errors.Is(cause, context.Canceled): // the server shuts down
		ws.Close(websocket.StatusGoingAway, "server stopping")
	default:
		ws.CloseNow() // the browser went away or broke the protocol
	}
}

func hangupTerm(p *os.Process) {
	if p == nil {
		return
	}
	if err := p.Signal(syscall.SIGHUP); err != nil {
		_ = p.Kill()
	}
}

// termEnv is the environment for the attach: ours minus what would confuse
// it (our TERM, Claude Code's nesting guard), as an xterm-256color.
func termEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "TERM", "COLORTERM", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_CHILD_SESSION", "HERDR_SOCKET_PATH":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func termDim(s string, def, lo, hi uint16) uint16 {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return def
	}
	return min(max(uint16(n), lo), hi)
}

func clamp(n, lo, hi int) int { return min(max(n, lo), hi) }

// trimReason fits a close reason in the 123 bytes a close frame allows.
func trimReason(s string) string {
	if len(s) <= 120 {
		return s
	}
	return strings.ToValidUTF8(s[:117], "") + "..."
}
