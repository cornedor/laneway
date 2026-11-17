package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeServer answers each connection's single request with handle's lines.
func fakeServer(t *testing.T, handle func(method string, params json.RawMessage) []string) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "h.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				_ = json.Unmarshal(line, &req)
				for _, l := range handle(req.Method, req.Params) {
					_, _ = conn.Write([]byte(l + "\n"))
				}
				time.Sleep(50 * time.Millisecond)
			}()
		}
	}()
	return New(path)
}

func TestAgents(t *testing.T) {
	c := fakeServer(t, func(method string, _ json.RawMessage) []string {
		if method != "agent.list" {
			t.Errorf("method = %q", method)
		}
		return []string{`{"id":"x","result":{"type":"agent_list","agents":[{"pane_id":"w1:p1","workspace_id":"w1","agent":"claude","agent_status":"blocked","cwd":"/src/a","terminal_title_stripped":"JB-1 fix"}]}}`}
	})
	got, err := c.Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PaneID != "w1:p1" || got[0].Status != Blocked || got[0].Title != "JB-1 fix" {
		t.Fatalf("got %+v", got)
	}
}

func TestStartAgentWithoutArgs(t *testing.T) {
	c := fakeServer(t, func(method string, p json.RawMessage) []string {
		var params struct {
			Args []string `json:"args"`
		}
		_ = json.Unmarshal(p, &params)
		if method != "agent.start" || params.Args == nil {
			return []string{`{"id":"x","error":{"code":"invalid_request","message":"invalid type: null, expected a sequence"}}`}
		}
		return []string{`{"id":"x","result":{"type":"agent_started"}}`}
	})
	if err := c.StartAgent(context.Background(), "jira-prot-1", "claude", "w1:p1", nil); err != nil {
		t.Fatal(err)
	}
}

func TestNewTabWithoutEnv(t *testing.T) {
	c := fakeServer(t, func(method string, p json.RawMessage) []string {
		var params struct {
			Env map[string]string `json:"env"`
		}
		_ = json.Unmarshal(p, &params)
		if method != "tab.create" || params.Env == nil {
			return []string{`{"id":"x","error":{"code":"invalid_request","message":"invalid type: null, expected a map"}}`}
		}
		return []string{`{"id":"x","result":{"type":"tab_created","tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p2"}}}`}
	})
	tab, pane, err := c.NewTab(context.Background(), "w1", "PROT-1", "/src/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tab != "w1:t2" || pane != "w1:p2" {
		t.Fatalf("tab, pane = %q, %q", tab, pane)
	}
}

func TestPromptError(t *testing.T) {
	c := fakeServer(t, func(string, json.RawMessage) []string {
		return []string{`{"id":"x","error":{"code":"agent_blocked","message":"agent is blocked"}}`}
	})
	err := c.Prompt(context.Background(), "w1:p1", "hi")
	if !IsCode(err, "agent_blocked") {
		t.Fatalf("err = %v", err)
	}
}

func TestSubscribe(t *testing.T) {
	var params json.RawMessage
	c := fakeServer(t, func(_ string, p json.RawMessage) []string {
		params = p
		return []string{
			`{"id":"x","result":{"type":"subscription_started"}}`,
			`{"event":"pane.agent_status_changed","data":{"type":"pane_agent_status_changed","pane_id":"w1:p1","workspace_id":"w1","agent_status":"done"}}`,
			`{"event":"pane.updated","data":{"type":"pane_updated","pane":{"pane_id":"w1:p2"}}}`,
		}
	})
	s, err := c.Subscribe(context.Background(), []string{"w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev, err := s.Next()
	if err != nil || ev.PaneID != "w1:p1" || ev.Status != Done {
		t.Fatalf("ev = %+v, %v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Kind != "pane.updated" || ev.PaneID != "w1:p2" {
		t.Fatalf("ev = %+v, %v", ev, err)
	}
	var p struct {
		Subscriptions []map[string]string `json:"subscriptions"`
	}
	_ = json.Unmarshal(params, &p)
	last := p.Subscriptions[len(p.Subscriptions)-1]
	if last["type"] != "pane.agent_status_changed" || last["pane_id"] != "w1:p1" {
		t.Fatalf("subscriptions = %v", p.Subscriptions)
	}
}

func TestWorktree(t *testing.T) {
	c := fakeServer(t, func(method string, params json.RawMessage) []string {
		switch method {
		case "worktree.open":
			return []string{`{"id":"x","error":{"code":"worktree_not_found","message":"worktree branch not found"}}`}
		case "worktree.create":
			var p map[string]string
			_ = json.Unmarshal(params, &p)
			if p["branch"] != "issue/X-1-a" || p["base"] != "origin/main" {
				t.Errorf("params = %v", p)
			}
			return []string{`{"id":"x","result":{"type":"worktree_created","root_pane":{"pane_id":"w2:p1","tab_id":"w2:t1"},"workspace":{"workspace_id":"w2","label":"issue-x-1-a"},"worktree":{"path":"/wt/issue-x-1-a","branch":"issue/X-1-a"}}}`}
		}
		t.Errorf("method = %q", method)
		return nil
	})
	ctx := context.Background()
	if _, err := c.OpenWorktree(ctx, "/src/x", "issue/X-1-a"); !IsCode(err, "worktree_not_found") {
		t.Fatalf("open err = %v", err)
	}
	wt, err := c.CreateWorktree(ctx, "/src/x", "issue/X-1-a", "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if wt != (Worktree{Path: "/wt/issue-x-1-a", Workspace: "w2", Tab: "w2:t1", Pane: "w2:p1"}) {
		t.Errorf("worktree = %+v", wt)
	}
}

func TestAtNeedsASocket(t *testing.T) {
	if At(filepath.Join(t.TempDir(), "missing.sock")) != nil {
		t.Fatal("At must be nil without a socket")
	}
	path := filepath.Join(t.TempDir(), "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if c := At(path); c == nil || c.Path() != path {
		t.Fatalf("At(%s) = %v", path, c)
	}
}
