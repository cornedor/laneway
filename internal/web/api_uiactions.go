package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/llm"
	"github.com/cornedor/laneway/internal/safeterm"
)

// Your own actions (ui.actions) run here with the issue as JSON on stdin and
// LANEWAY_KEY / LANEWAY_KEYS in the environment, as in the TUI. The browser
// names an action by its index in the list this file serves, never a command.

const (
	actionTimeout = time.Minute
	maxQuestion   = 2000
)

type actionIssue struct {
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Assignee string `json:"assignee"`
	Priority string `json:"priority"`
	Points   string `json:"points"`
	URL      string `json:"url"`
}

// usableActions are the configured actions that have a name and a command
// and a known where and show. Indexes into ui.actions stay the ids.
func usableActions(as []config.Action) map[int]config.Action {
	out := map[int]config.Action{}
	for i, a := range as {
		if a.Name == "" || len(a.Command) == 0 ||
			!slices.Contains([]string{"", "board", "panel", "both"}, a.Where) ||
			!slices.Contains([]string{"", "status", "pager"}, a.Show) {
			continue
		}
		out[i] = a
	}
	return out
}

func init() {
	get("/actions", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		type item struct {
			ID          int
			Name, Key   string
			Where, Show string
			Refresh     bool
		}
		out := []item{}
		ok := usableActions(s.UIConfig().Actions)
		for i, a := range s.UIConfig().Actions {
			if _, yes := ok[i]; yes {
				out = append(out, item{i, a.Name, a.Key, a.Where, a.Show, a.Refresh})
			}
		}
		return out, nil
	})
	post("/actions/{id}/run", runAction)
	get("/ask", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		cmd := llm.Command(strings.Fields(s.UIConfig().LLM))
		name := ""
		if len(cmd) > 0 {
			name = filepath.Base(cmd[0])
		}
		return map[string]any{"Available": cmd != nil, "Command": name, "Asks": llm.Asks}, nil
	})
	handle("POST /api/issues/{key}/ask", askIssue)
}

func runAction(ctx context.Context, s *Server, r *http.Request) (any, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	a, ok := usableActions(s.UIConfig().Actions)[id]
	if err != nil || !ok {
		return nil, httpError{http.StatusNotFound, "no such action"}
	}
	b, err := Body[struct{ Keys []string }](r)
	if err != nil {
		return nil, err
	}
	if len(b.Keys) == 0 || len(b.Keys) > 200 {
		return nil, badRequest("no issue")
	}
	c := s.Client()
	var issues []actionIssue
	for _, k := range b.Keys {
		if !jira.ValidKey(k) {
			return nil, badRequest("bad issue key " + k)
		}
		is, err := c.Get(ctx, k)
		if err != nil {
			return nil, err
		}
		issues = append(issues, actionIssue{is.Key, is.Summary, is.Status, is.Type, is.Assignee, is.Priority, is.StoryPoints, c.BrowseURL(is.Key)})
	}
	var in []byte
	if len(issues) == 1 {
		in, _ = json.Marshal(issues[0])
	} else {
		in, _ = json.Marshal(issues)
	}
	ctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.Command[0], a.Command[1:]...)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Env = append(os.Environ(), "LANEWAY_KEY="+b.Keys[0], "LANEWAY_KEYS="+strings.Join(b.Keys, " "))
	out, err := cmd.CombinedOutput()
	res := map[string]any{"Name": a.Name, "Output": safeterm.Text(strings.TrimRight(string(out), "\n")), "Show": a.Show, "Refresh": a.Refresh && err == nil}
	if err != nil {
		res["Error"] = err.Error()
	}
	return res, nil
}

// askIssue streams ui.llm's answer about the issue as server-sent events:
// `data: "chunk"` (a JSON string), then `event: done`, or `event: error`.
// Body: {Question: id of a canned question} or {Text: free text}.
func askIssue(s *Server, w http.ResponseWriter, r *http.Request) {
	fail := func(code int, msg string) { writeErr(w, httpError{code, msg}) }
	key := r.PathValue("key")
	if !jira.ValidKey(key) {
		fail(http.StatusBadRequest, "bad issue key")
		return
	}
	b, err := Body[struct{ Question, Text string }](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	command := llm.Command(strings.Fields(s.UIConfig().LLM))
	if command == nil {
		fail(http.StatusNotImplemented, "asking needs ui.llm (claude -p, llm, ollama run …) or claude on the PATH")
		return
	}
	prompt := strings.TrimSpace(b.Text)
	for _, a := range llm.Asks {
		if a.ID == b.Question {
			prompt = a.Prompt
			b.Text = ""
		}
	}
	if prompt == "" {
		fail(http.StatusBadRequest, "no question")
		return
	}
	if b.Text != "" && (strings.HasPrefix(prompt, "-") || utf8.RuneCountInString(prompt) > maxQuestion) {
		fail(http.StatusBadRequest, "question must not start with - and is at most 2000 characters")
		return
	}
	c := s.Client()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	iss, err := c.Get(ctx, key)
	if err != nil {
		writeErr(w, err)
		return
	}
	hist, _ := c.History(ctx, key) // the issue alone still answers
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	sw := &sseWriter{w: w, fl: fl}
	err = llm.Stream(ctx, command, prompt, llm.Input(*iss, hist), sw)
	_ = sw.Flush()
	if err != nil {
		e, _ := json.Marshal(err.Error())
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", e)
	} else {
		fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}
	if fl != nil {
		fl.Flush()
	}
}

// sseWriter sends each write as one data event. A multi-byte rune cut by the
// pipe is held back until its rest arrives.
type sseWriter struct {
	w    http.ResponseWriter
	fl   http.Flusher
	rest []byte
}

func (s *sseWriter) Write(p []byte) (int, error) {
	buf := append(s.rest, p...)
	s.rest = nil
	cut := len(buf)
	for i := 1; i < utf8.UTFMax && i <= len(buf); i++ {
		c := buf[len(buf)-i]
		if utf8.RuneStart(c) {
			if c >= 0xC0 && !utf8.FullRune(buf[len(buf)-i:]) {
				cut = len(buf) - i
			}
			break
		}
	}
	s.rest = append([]byte(nil), buf[cut:]...)
	if cut == 0 {
		return len(p), nil
	}
	return len(p), s.send(buf[:cut])
}

// Flush sends what is held back (invalid or cut off for good).
func (s *sseWriter) Flush() error {
	if len(s.rest) == 0 {
		return nil
	}
	b := s.rest
	s.rest = nil
	return s.send(b)
}

func (s *sseWriter) send(p []byte) error {
	b, _ := json.Marshal(string(p))
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	if s.fl != nil {
		s.fl.Flush()
	}
	return nil
}
