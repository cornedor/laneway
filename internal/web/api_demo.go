package web

import (
	"net/http"
	"regexp"
	"strings"
)

// Demo mode has no business touching the user's machine: herdr agents,
// git repos, configured commands, gh, glab and the LLM are all off limits. An issue's
// branch name is only its template filled in, so ctrl+y works.

var demoIssueBlocked = regexp.MustCompile(`^/api/issues/[^/]+/(ask|work|pr|worktree)$`)

// demoGate answers the machine-touching routes in demo mode. It reports
// whether the request was handled.
func demoGate(s *Server, w http.ResponseWriter, r *http.Request) bool {
	if !s.opt.Demo {
		return false
	}
	p := r.URL.Path
	if r.Method == http.MethodGet {
		var empty any
		switch p {
		case "/api/agents/status":
			empty = map[string]any{"Available": false, "CLI": false}
		case "/api/agents":
			empty = AgentsSnapshot{Agents: []AgentOut{}, Worktrees: map[string]string{}}
		case "/api/actions":
			empty = []any{}
		case "/api/ask":
			empty = map[string]any{"Available": false, "Command": "", "Asks": []any{}}
		case "/api/branch":
			empty = map[string]string{}
		case "/api/review": // gh and glab ask the user's own forges
			empty = map[string]any{"Keys": []string{}, "Cards": []any{}, "Requests": 0}
		case "/api/worklog/proposals":
			empty = map[string]any{"Items": []any{}, "Failed": []string{}}
		}
		if empty != nil {
			writeJSON(w, r, empty)
			return true
		}
	}
	if r.Method == http.MethodGet && p == "/api/agents/events" {
		fl, ok := w.(http.Flusher)
		if !ok {
			return false
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("data: {\"Available\":false,\"Agents\":[],\"Worktrees\":{}}\n\n"))
		fl.Flush()
		<-r.Context().Done()
		return true
	}
	if strings.HasPrefix(p, "/api/agents/") || strings.HasPrefix(p, "/api/actions/") || demoIssueBlocked.MatchString(p) {
		writeErr(w, httpError{http.StatusServiceUnavailable, "not available in demo"})
		return true
	}
	return false
}
