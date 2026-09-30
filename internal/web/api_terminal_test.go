package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cornedor/laneway/internal/herdr"
)

// fakeAttach stands in for `herdr agent attach`: a shell on the pty that
// says what it was run with, echoes lines, reports its size and notes a
// hangup in mark.
func fakeAttach(t *testing.T) (bin, mark string) {
	t.Helper()
	dir := t.TempDir()
	mark = filepath.Join(dir, "hup")
	bin = filepath.Join(dir, "herdr")
	script := `#!/bin/sh
trap 'echo hup > "` + mark + `"; exit 0' HUP
echo "args:$*"
echo "env:$TERM:$HERDR_SOCKET_PATH"
stty size
while IFS= read -r line; do
  case "$line" in
    size) stty size ;;
    quit) exit 3 ;;
    *) echo "got:$line" ;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	return bin, mark
}

// termServer serves the API with the fake herdr socket and attach.
func termServer(t *testing.T, opt Options) (*httptest.Server, *herdr.Client, string) {
	t.Helper()
	_, c := newFakeHerdr(t)
	bin, mark := fakeAttach(t)
	oldC, oldBin := herdrClient, herdrBin
	herdrClient, herdrBin = func() *herdr.Client { return c }, bin
	t.Cleanup(func() { herdrClient, herdrBin = oldC, oldBin })
	ts := httptest.NewServer(New(context.Background(), opt))
	t.Cleanup(ts.Close)
	return ts, c, mark
}

func dialTerm(t *testing.T, ts *httptest.Server, path string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if hdr == nil {
		hdr = http.Header{"Origin": {ts.URL}}
	}
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+path, &websocket.DialOptions{HTTPHeader: hdr})
}

// readUntil collects output until it holds want.
func readUntil(t *testing.T, ws *websocket.Conn, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out strings.Builder
	for !strings.Contains(out.String(), want) {
		typ, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v; got %q", want, err, out.String())
		}
		if typ == websocket.MessageBinary {
			out.Write(b)
		}
	}
	return out.String()
}

func TestTerminalBridge(t *testing.T) {
	ts, c, mark := termServer(t, Options{})
	ws, _, err := dialTerm(t, ts, "/api/agents/p1/terminal?cols=90&rows=20", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	out := readUntil(t, ws, "20 90")
	if !strings.Contains(out, "args:agent attach p1") || !strings.Contains(out, "env:xterm-256color:"+c.Path()) {
		t.Errorf("start = %q", out)
	}
	ctx := context.Background()
	if err := ws.Write(ctx, websocket.MessageBinary, []byte("hello é\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "got:hello é")
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":100,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	_ = ws.Write(ctx, websocket.MessageBinary, []byte("size\r"))
	readUntil(t, ws, "30 100")
	// Out of range sizes are clamped.
	_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":99999,"rows":0}`))
	_ = ws.Write(ctx, websocket.MessageBinary, []byte("size\r"))
	readUntil(t, ws, "3 500")

	// Closing the socket hangs up the attach, not the agent.
	ws.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, _ := os.ReadFile(mark); strings.TrimSpace(string(b)) == "hup" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("attach was not hung up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitNoTerms(t)
}

func waitNoTerms(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		termMu.Lock()
		n, m := termCount, len(termPanes)
		termMu.Unlock()
		if n == 0 && m == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessions left: %d, panes %d", n, m)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func closeCode(t *testing.T, ws *websocket.Conn) (websocket.StatusCode, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var text string
	for {
		typ, b, err := ws.Read(ctx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				return ce.Code, text
			}
			t.Fatalf("read: %v", err)
		}
		if typ == websocket.MessageText {
			text = string(b)
		}
	}
}

func TestTerminalExitAndTakeover(t *testing.T) {
	ts, _, _ := termServer(t, Options{})
	ws, _, err := dialTerm(t, ts, "/api/agents/p1/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "24 80")
	_ = ws.Write(context.Background(), websocket.MessageBinary, []byte("quit\r"))
	if code, text := closeCode(t, ws); code != termExited || !strings.Contains(text, `"exit"`) || !strings.Contains(text, "exit status 3") {
		t.Errorf("exit = %d %q", code, text)
	}

	first, _, err := dialTerm(t, ts, "/api/agents/p1/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, first, "24 80")
	second, _, err := dialTerm(t, ts, "/api/agents/p1/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.CloseNow()
	if code, _ := closeCode(t, first); code != termTakenOver {
		t.Errorf("first after a second = %d, want taken over", code)
	}
	readUntil(t, second, "24 80")
	second.Close(websocket.StatusNormalClosure, "")
	waitNoTerms(t)
}

func TestTerminalIdle(t *testing.T) {
	oldIdle, oldTick := termIdle, termTick
	termIdle, termTick = 150*time.Millisecond, 30*time.Millisecond
	defer func() { termIdle, termTick = oldIdle, oldTick }()
	ts, _, _ := termServer(t, Options{})
	ws, _, err := dialTerm(t, ts, "/api/agents/p1/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "24 80")
	if code, _ := closeCode(t, ws); code != termIdleClose {
		t.Errorf("idle = %d", code)
	}
	waitNoTerms(t)
}

// The upgrade goes through the same guards as the API, and needs a
// same-origin Origin besides.
func TestTerminalRefused(t *testing.T) {
	ts, _, _ := termServer(t, Options{})
	status := func(path string, hdr http.Header) int {
		ws, res, err := dialTerm(t, ts, path, hdr)
		if err == nil {
			ws.CloseNow()
			return 101
		}
		if res == nil {
			t.Fatalf("%s: %v", path, err)
		}
		return res.StatusCode
	}
	p := "/api/agents/p1/terminal"
	for name, c := range map[string]struct {
		path string
		hdr  http.Header
		want int
	}{
		"no origin":    {p, http.Header{"X": {"y"}}, 403},
		"cross origin": {p, http.Header{"Origin": {"http://evil.example"}}, 403},
		"cross site":   {p, http.Header{"Origin": {ts.URL}, "Sec-Fetch-Site": {"cross-site"}}, 403},
		"unknown pane": {"/api/agents/nope/terminal", nil, 404},
		"flag pane":    {"/api/agents/-x/terminal", nil, 400},
	} {
		if got := status(c.path, c.hdr); got != c.want {
			t.Errorf("%s = %d, want %d", name, got, c.want)
		}
	}
	waitNoTerms(t)

	// A rebound Host, the launch token and demo mode, straight on the handler.
	up := map[string]string{"Origin": "http://localhost", "Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}
	if c := secReq(New(context.Background(), Options{}), "GET", p, "evil.example", map[string]string{"Origin": "http://evil.example"}).Code; c != 403 {
		t.Errorf("rebound host = %d", c)
	}
	if c := secReq(New(context.Background(), Options{Token: "sekret"}), "GET", p, "localhost", up).Code; c != 401 {
		t.Errorf("no token cookie = %d", c)
	}
	if c := secReq(New(context.Background(), Options{Demo: true}), "GET", p, "localhost", up).Code; c != 503 {
		t.Errorf("demo = %d", c)
	}
	herdrClient = func() *herdr.Client { return nil }
	if c := secReq(New(context.Background(), Options{}), "GET", p, "localhost", up).Code; c != 503 {
		t.Errorf("no herdr = %d", c)
	}
}
