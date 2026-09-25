// Package herdr talks to a running herdr server (the terminal workspace
// manager for coding agents) over its socket API: newline-delimited JSON, one
// request per line, one response per line, with event subscriptions keeping
// the connection open for pushed lines.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
)

// Status is an agent's lifecycle state as herdr reports it.
type Status string

const (
	Working Status = "working"
	Blocked Status = "blocked" // an approval or question UI is up
	Done    Status = "done"    // finished, not yet seen
	Idle    Status = "idle"
	Unknown Status = "unknown"
)

// Agent is one agent pane.
type Agent struct {
	PaneID      string `json:"pane_id"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Agent       string `json:"agent"`
	Status      Status `json:"agent_status"`
	CWD         string `json:"cwd"`
	Title       string `json:"terminal_title_stripped"`
	Focused     bool   `json:"focused"`
	Seq         uint64 `json:"state_change_seq"`
}

// Workspace is one herdr workspace.
type Workspace struct {
	ID    string `json:"workspace_id"`
	Label string `json:"label"`
}

// Event is one pushed subscription line. PaneID and Status are set for the
// pane events that carry them.
type Event struct {
	Kind   string
	PaneID string
	Status Status
}

// Error is an error response from the server.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// IsCode reports whether err is a server error with the given code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// Client addresses one herdr server by its socket path.
type Client struct{ path string }

// Default finds the socket the herdr CLI would use: HERDR_SOCKET_PATH, then
// HERDR_SESSION's, then the default session's. Nil when there is none.
func Default() *Client {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(dir, "herdr", "herdr.sock")
		if s := os.Getenv("HERDR_SESSION"); s != "" {
			path = filepath.Join(dir, "herdr", "sessions", s, "herdr.sock")
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode()&os.ModeSocket == 0 {
		return nil
	}
	return &Client{path: path}
}

// New addresses the socket at path.
func New(path string) *Client { return &Client{path: path} }

// Path is the socket path, for handing to a herdr CLI child.
func (c *Client) Path() string { return c.path }

var reqID atomic.Int64

func (c *Client) dial(ctx context.Context, method string, params any) (net.Conn, *bufio.Reader, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, nil, err
	}
	if params == nil {
		params = struct{}{}
	}
	req, err := json.Marshal(map[string]any{
		"id":     "matterbox:" + strconv.FormatInt(reqID.Add(1), 10),
		"method": method,
		"params": params,
	})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, bufio.NewReaderSize(conn, 64*1024), nil
}

// readResult reads one response line into out (the "result" object).
func readResult(r *bufio.Reader, out any) error {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("herdr: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

// call sends one request and decodes its result into out.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	conn, r, err := c.dial(ctx, method, params)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := readResult(r, out); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

// Agents lists every agent pane.
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	var res struct {
		Agents []Agent `json:"agents"`
	}
	err := c.call(ctx, "agent.list", nil, &res)
	return res.Agents, err
}

// Workspaces lists the workspaces, for their labels.
func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var res struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.call(ctx, "workspace.list", nil, &res)
	return res.Workspaces, err
}

// Prompt submits text to the agent in pane, followed by Enter. herdr refuses
// an agent sitting at an approval or question with code "agent_blocked".
func (c *Client) Prompt(ctx context.Context, pane, text string) error {
	return c.call(ctx, "agent.prompt", map[string]any{"target": pane, "text": text}, nil)
}

// Workspace returns the id of the workspace labelled label, creating it at cwd
// when there is none.
func (c *Client) Workspace(ctx context.Context, label, cwd string) (string, error) {
	wss, err := c.Workspaces(ctx)
	if err != nil {
		return "", err
	}
	for _, ws := range wss {
		if ws.Label == label {
			return ws.ID, nil
		}
	}
	var res struct {
		Workspace Workspace `json:"workspace"`
	}
	err = c.call(ctx, "workspace.create", map[string]any{"label": label, "cwd": cwd}, &res)
	return res.Workspace.ID, err
}

// NewTab opens an unfocused tab in workspace, its shell started in cwd with
// env added, and returns the tab and its pane.
func (c *Client) NewTab(ctx context.Context, workspace, label, cwd string, env map[string]string) (tab, pane string, err error) {
	var res struct {
		Tab struct {
			ID string `json:"tab_id"`
		} `json:"tab"`
		RootPane struct {
			ID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	err = c.call(ctx, "tab.create", map[string]any{
		"workspace_id": workspace, "label": label, "cwd": cwd, "env": env,
	}, &res)
	return res.Tab.ID, res.RootPane.ID, err
}

// StartAgent launches the kind's executable with args in pane, which must be
// at its shell prompt, and waits until it takes input.
func (c *Client) StartAgent(ctx context.Context, name, kind, pane string, args []string) error {
	return c.call(ctx, "agent.start", map[string]any{
		"name": name, "kind": kind, "pane_id": pane, "args": args,
	}, nil)
}

// Worktree is a checkout opened as a herdr workspace, with its first tab.
type Worktree struct {
	Path        string
	Workspace   string
	Tab, Pane   string
	AlreadyOpen bool
}

type worktreeResult struct {
	AlreadyOpen bool `json:"already_open"`
	RootPane    struct {
		ID  string `json:"pane_id"`
		Tab string `json:"tab_id"`
	} `json:"root_pane"`
	Workspace Workspace `json:"workspace"`
	Worktree  struct {
		Path string `json:"path"`
	} `json:"worktree"`
}

func (r worktreeResult) worktree() Worktree {
	return Worktree{
		Path: r.Worktree.Path, Workspace: r.Workspace.ID,
		Tab: r.RootPane.Tab, Pane: r.RootPane.ID, AlreadyOpen: r.AlreadyOpen,
	}
}

// OpenWorktree opens the repo's existing worktree on branch as a workspace,
// or finds the one already open. herdr answers "worktree_not_found" when the
// branch has no worktree.
func (c *Client) OpenWorktree(ctx context.Context, repo, branch string) (Worktree, error) {
	var res worktreeResult
	err := c.call(ctx, "worktree.open", map[string]any{"cwd": repo, "branch": branch}, &res)
	return res.worktree(), err
}

// CreateWorktree checks branch out in a new worktree of repo, opened as a
// workspace. The branch is created from base ("" for herdr's default) unless
// it exists.
func (c *Client) CreateWorktree(ctx context.Context, repo, branch, base string) (Worktree, error) {
	params := map[string]any{"cwd": repo, "branch": branch}
	if base != "" {
		params["base"] = base
	}
	var res worktreeResult
	err := c.call(ctx, "worktree.create", params, &res)
	return res.worktree(), err
}

// RenameTab relabels a tab.
func (c *Client) RenameTab(ctx context.Context, tab, label string) error {
	return c.call(ctx, "tab.rename", map[string]any{"tab_id": tab, "label": label}, nil)
}

// CloseTab closes a tab and whatever runs in it.
func (c *Client) CloseTab(ctx context.Context, tab string) error {
	return c.call(ctx, "tab.close", map[string]any{"tab_id": tab}, nil)
}

// Stream is a live event subscription.
type Stream struct {
	conn net.Conn
	r    *bufio.Reader
}

// Subscribe opens a subscription to agents appearing and panes closing or
// changing (titles), plus the status of each pane in panes: herdr scopes
// status events to one pane per subscription.
func (c *Client) Subscribe(ctx context.Context, panes []string) (*Stream, error) {
	subs := []map[string]string{
		{"type": "pane.agent_detected"},
		{"type": "pane.closed"},
		{"type": "pane.exited"},
		{"type": "pane.updated"},
	}
	for _, p := range panes {
		subs = append(subs, map[string]string{"type": "pane.agent_status_changed", "pane_id": p})
	}
	conn, r, err := c.dial(ctx, "events.subscribe", map[string]any{"subscriptions": subs})
	if err != nil {
		return nil, err
	}
	if err := readResult(r, nil); err != nil {
		conn.Close()
		return nil, err
	}
	return &Stream{conn: conn, r: r}, nil
}

// Next blocks for the next pushed event. It fails once the stream is closed
// or the server goes away.
func (s *Stream) Next() (Event, error) {
	line, err := s.r.ReadBytes('\n')
	if err != nil {
		return Event{}, err
	}
	var env struct {
		Event string `json:"event"`
		Data  struct {
			PaneID string `json:"pane_id"`
			Status Status `json:"agent_status"`
			Pane   struct {
				PaneID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"data"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return Event{}, fmt.Errorf("herdr: %w", err)
	}
	ev := Event{Kind: env.Event, PaneID: env.Data.PaneID, Status: env.Data.Status}
	if ev.PaneID == "" {
		ev.PaneID = env.Data.Pane.PaneID
	}
	return ev, nil
}

// Close ends the subscription.
func (s *Stream) Close() error { return s.conn.Close() }
