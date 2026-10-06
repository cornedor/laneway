package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// TestTerminalManual serves the web UI with a stand-in herdr for trying the
// browser terminal by hand: the demo Jira, a fake herdr socket with agents
// on DEMO-4 and DEMO-5, and an "attach" that runs a local shell (or
// LANEWAY_TERM_CMD) on the pty. Skipped unless LANEWAY_TERM_MANUAL is the
// address to listen on:
//
//	LANEWAY_TERM_MANUAL=127.0.0.1:8592 go test -run TestTerminalManual -timeout 0 ./internal/web/
//
// With LANEWAY_TERM_REAL=1 it uses the herdr of HERDR_SOCKET_PATH instead:
// point that at a throwaway `herdr --session x server`, not your own.
func TestTerminalManual(t *testing.T) {
	addr := os.Getenv("LANEWAY_TERM_MANUAL")
	if addr == "" {
		t.Skip("set LANEWAY_TERM_MANUAL=host:port to run")
	}
	if os.Getenv("LANEWAY_TERM_REAL") == "" {
		fakeHerdrForManual(t)
	}
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	st, _ := store.Open(filepath.Join(t.TempDir(), "state.json"))
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	conf := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(conf, []byte("ui: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	h := New(ctx, Options{Client: cl, Store: st, Site: "demo", ConfigPath: conf})
	t.Logf("serving http://%s", addr)
	if err := Serve(ctx, addr, false, "", "", h, nil); err != nil {
		t.Fatal(err)
	}
}

func fakeHerdrForManual(t *testing.T) {
	f, c := newFakeHerdr(t)
	f.mu.Lock()
	f.agents = append(f.agents, map[string]any{
		"pane_id": "p2", "name": "jira-demo-5-xyz", "workspace_id": "w2", "tab_id": "t2", "agent": "claude",
		"agent_status": "blocked", "cwd": "/w/demo-5", "terminal_title_stripped": "approve?",
	})
	f.mu.Unlock()
	cmd := os.Getenv("LANEWAY_TERM_CMD")
	if cmd == "" {
		cmd = "PS1='agent \\w $ ' exec bash --norc -i"
	}
	bin := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexport LANEWAY_PANE=\"$3\"\n"+cmd+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldC, oldBin := herdrClient, herdrBin
	herdrClient, herdrBin = func() *herdr.Client { return c }, bin
	t.Cleanup(func() { herdrClient, herdrBin = oldC, oldBin })
	t.Logf("terminal stand-in: %s", cmd)
}
