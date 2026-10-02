package demo

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A GitLab behind the demo's GitLab merge requests (shop-api): the API the
// merge request views read (internal/forge/gitlab), and its writes kept in
// memory, so the panel's merge request, its diff and the review try out
// with nothing real behind them. !87 is the one with a pipeline, versions,
// threads and a file to expand; the others are short.

// gitlabMe is the demo user as GitLab knows them.
const gitlabMe = "jamie"

var (
	mrRe     = regexp.MustCompile(`^/api/v4/projects/acme/([a-z-]+)/merge_requests/(\d+)(/.*)?$`)
	mrFileRe = regexp.MustCompile(`^/api/v4/projects/acme/([a-z-]+)/repository/files/(.+)/raw$`)
	jobsRe   = regexp.MustCompile(`^/api/v4/projects/\d+/pipelines/(\d+)/jobs$`)
)

// gitlabState is what the demo's writes changed: notes added, drafts
// pending, threads resolved, approvals given.
type gitlabState struct {
	notes    map[int][]gitlabThread    // by iid, after the built-in ones
	replies  map[string][]gitlabThread // by thread id
	drafts   map[int][]gitlabDraft
	resolved map[string]bool
	approved map[int]bool
	seq      int
}

type gitlabThread struct {
	id      string
	author  user
	body    string
	path    string
	newLine int
	oldLine int
	at      time.Time
	replies []gitlabThread // the notes after the first
}

type gitlabDraft struct {
	id      int
	body    string
	replyTo string
	pos     map[string]any
}

// The merge request with something to review.
const (
	shippingOld  = "package shipping\n\nimport \"net/http\"\n\n// Rates answers the shipping rates for a cart.\nfunc Rates(w http.ResponseWriter, r *http.Request) {\n\tregion := r.URL.Query().Get(\"region\")\n\tband := weightBand(r)\n\trate, err := carrier.Lookup(region, band)\n\tif err != nil {\n\t\thttp.Error(w, err.Error(), http.StatusBadGateway)\n\t\treturn\n\t}\n\twriteJSON(w, rate)\n}\n"
	shippingNew  = "package shipping\n\nimport \"net/http\"\n\n// Rates answers the shipping rates for a cart, from the cache when it can.\nfunc Rates(w http.ResponseWriter, r *http.Request) {\n\tregion := r.URL.Query().Get(\"region\")\n\tband := weightBand(r)\n\trate, ok := cache.Get(region, band)\n\tif !ok {\n\t\tvar err error\n\t\tif rate, err = carrier.Lookup(region, band); err != nil {\n\t\t\thttp.Error(w, err.Error(), http.StatusBadGateway)\n\t\t\treturn\n\t\t}\n\t\tcache.Put(region, band, rate)\n\t}\n\twriteJSON(w, rate)\n}\n"
	shippingDiff = "@@ -5 +5 @@\n-// Rates answers the shipping rates for a cart.\n+// Rates answers the shipping rates for a cart, from the cache when it can.\n" +
		"@@ -8,7 +8,11 @@\n \tband := weightBand(r)\n-\trate, err := carrier.Lookup(region, band)\n-\tif err != nil {\n-\t\thttp.Error(w, err.Error(), http.StatusBadGateway)\n-\t\treturn\n-\t}\n" +
		"+\trate, ok := cache.Get(region, band)\n+\tif !ok {\n+\t\tvar err error\n+\t\tif rate, err = carrier.Lookup(region, band); err != nil {\n+\t\t\thttp.Error(w, err.Error(), http.StatusBadGateway)\n+\t\t\treturn\n+\t\t}\n+\t\tcache.Put(region, band, rate)\n+\t}\n \twriteJSON(w, rate)\n"
	cacheNew = "package shipping\n\nimport (\n\t\"sync\"\n\t\"time\"\n)\n\n// rateCache keeps carrier rates by region and weight band until the cut-off.\ntype rateCache struct {\n\tmu    sync.Mutex\n\trates map[string]Rate\n\tuntil time.Time\n}\n\nvar cache = &rateCache{rates: map[string]Rate{}}\n"
)

// gitlabFiles are a merge request's changes, per version (1 the first push).
func gitlabFiles(iid, version int) []map[string]any {
	added := func(text string) string {
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		return fmt.Sprintf("@@ -0,0 +1,%d @@\n+", len(lines)) + strings.Join(lines, "\n+") + "\n"
	}
	file := func(path, diff string, isNew bool) map[string]any {
		return map[string]any{"old_path": path, "new_path": path, "new_file": isNew, "diff": diff}
	}
	switch iid {
	case 87:
		if version == 1 {
			return []map[string]any{file("internal/shipping/cache.go", added(cacheNew), true)}
		}
		return []map[string]any{file("internal/shipping/cache.go", added(cacheNew), true), file("internal/shipping/shipping.go", shippingDiff, false)}
	case 81:
		return []map[string]any{file("mail/guest_confirmation.txt", added("Thanks for your order, {{.Name}}.\nTrack it here: {{.Link}}\n"), true)}
	default:
		return []map[string]any{file("api/v1/cart.go", "@@ -1,3 +0,0 @@\n-package v1\n-\n-// Cart is the legacy cart endpoint.\n", false)}
	}
}

// gitlabFile is a file of the merge request at its head, for expanding the
// diff around its changes.
func gitlabFile(path string) (string, bool) {
	switch path {
	case "internal/shipping/shipping.go":
		return shippingNew, true
	case "internal/shipping/cache.go":
		return cacheNew, true
	}
	return "", false
}

// gitlabMR is merge request iid of the demo's GitLab issues, nil for none.
func (s *Server) gitlabMR(iid int) (*devWork, *devPR) {
	for _, key := range []string{"DEMO-7", "DEMO-11", "DEMO-12"} {
		w := s.devWork(key)
		for i := range w.prs {
			if w.prs[i].n == iid {
				return w, &w.prs[i]
			}
		}
	}
	return nil, nil
}

func gitlabUser(u user) map[string]any {
	return map[string]any{"name": u.name, "username": strings.ToLower(strings.Fields(u.name)[0])}
}

// gitlabMRJSON is the merge request as the API sends it.
func (s *Server) gitlabMRJSON(w *devWork, p *devPR) map[string]any {
	state := map[string]string{"OPEN": "opened", "MERGED": "merged", "DECLINED": "closed"}[p.status]
	var revs []any
	for _, u := range p.reviewers {
		revs = append(revs, gitlabUser(u))
	}
	mr := map[string]any{"iid": p.n, "project_id": 1, "title": p.title, "state": state, "draft": false,
		"source_branch": p.src, "target_branch": p.dst, "author": gitlabUser(p.author), "reviewers": revs, "assignees": []any{gitlabUser(p.author)},
		"labels": []string{"backend"}, "changes_count": strconv.Itoa(len(gitlabFiles(p.n, 2))), "detailed_merge_status": "mergeable",
		"description": "Caches each region's carrier rate per weight band until the carrier's daily cut-off, so checkout stops waiting on the carrier for every cart.\n\n- [x] cache with cut-off\n- [x] hit-ratio metric\n- [ ] load test",
		"web_url":     s.repoURL(w) + "/-/merge_requests/" + strconv.Itoa(p.n), "updated_at": p.updated.UTC().Format(time.RFC3339),
		"user_notes_count": len(s.gitlabThreads(p.n)),
		"diff_refs":        map[string]any{"base_sha": "b4se0000", "start_sha": "b4se0000", "head_sha": headSHA(p.n, 2)},
		"references":       map[string]any{"full": "acme/" + w.repo + "!" + strconv.Itoa(p.n)}}
	if p.n == 87 {
		mr["head_pipeline"] = map[string]any{"id": 5521, "status": "success", "duration": 271, "web_url": s.repoURL(w) + "/-/pipelines/5521",
			"detailed_status": map[string]any{"label": "passed"}}
	}
	if p.status != "OPEN" {
		mr["detailed_merge_status"] = "not_open"
	}
	return mr
}

func headSHA(iid, version int) string { return fmt.Sprintf("%04dhead%d", iid, version) }

// gitlabThreads are iid's threads: the built-in ones, then the demo's, the
// replies posted since under theirs.
func (s *Server) gitlabThreads(iid int) []gitlabThread {
	var ts []gitlabThread
	if iid == 87 {
		ts = []gitlabThread{
			{id: "d87a", author: sam, body: "Should a carrier error also skip the cache? Today a failed lookup is retried on every cart.",
				path: "internal/shipping/shipping.go", newLine: 12, at: s.now.Add(-20 * time.Hour)},
			{id: "d87b", author: mira, body: "Looks good once the cut-off test is in.", at: s.now.Add(-5 * time.Hour)},
		}
	}
	ts = append(ts, s.git.notes[iid]...)
	for i := range ts {
		ts[i].replies = append(ts[i].replies, s.git.replies[ts[i].id]...)
	}
	return ts
}

func (t gitlabThread) json(resolved bool) map[string]any {
	var notes []any
	for i, n := range append([]gitlabThread{t}, t.replies...) {
		nj := map[string]any{"id": i + 1, "body": n.body, "author": gitlabUser(n.author), "created_at": n.at.UTC().Format(time.RFC3339),
			"system": false, "resolvable": true, "resolved": resolved}
		if t.path != "" {
			nj["position"] = map[string]any{"position_type": "text", "new_path": t.path, "old_path": t.path, "new_line": t.newLine, "old_line": t.oldLine, "head_sha": headSHA(87, 2)}
		}
		notes = append(notes, nj)
	}
	return map[string]any{"id": t.id, "notes": notes}
}

// serveGitLab answers /api/v4 requests; false for another path.
func (s *Server) serveGitLab(w http.ResponseWriter, r *http.Request, body map[string]any, send func(any)) bool {
	p, q := r.URL.Path, r.URL.Query()
	if !strings.HasPrefix(p, "/api/v4/") {
		return false
	}
	if s.git.notes == nil {
		s.git = gitlabState{notes: map[int][]gitlabThread{}, replies: map[string][]gitlabThread{}, drafts: map[int][]gitlabDraft{}, resolved: map[string]bool{}, approved: map[int]bool{}}
	}
	switch {
	case p == "/api/v4/user":
		send(gitlabUser(me))
		return true
	case p == "/api/v4/merge_requests":
		var out []any
		if q.Get("reviewer_username") == gitlabMe && q.Get("state") == "opened" {
			work, pr := s.gitlabMR(87)
			out = append(out, s.gitlabMRJSON(work, pr))
		}
		send(append([]any{}, out...))
		return true
	}
	if m := jobsRe.FindStringSubmatch(p); m != nil {
		send([]any{ // newest first, as GitLab lists them
			map[string]any{"name": "deploy:staging", "stage": "deploy", "status": "success"},
			map[string]any{"name": "docker", "stage": "build", "status": "success"},
			map[string]any{"name": "load-test", "stage": "test", "status": "failed", "allow_failure": true},
			map[string]any{"name": "lint", "stage": "test", "status": "success"},
			map[string]any{"name": "go test", "stage": "test", "status": "success"},
		})
		return true
	}
	if m := mrFileRe.FindStringSubmatch(p); m != nil {
		text, ok := gitlabFile(m[2])
		if !ok {
			send(map[string]any{"message": "404 File Not Found"})
			return true
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(text))
		return true
	}
	m := mrRe.FindStringSubmatch(p)
	if m == nil {
		return false
	}
	iid, _ := strconv.Atoi(m[2])
	work, pr := s.gitlabMR(iid)
	if work == nil {
		send(map[string]any{"message": "404 Not found"})
		return true
	}
	sub := m[3]
	switch {
	case sub == "" && r.Method == http.MethodGet:
		send(s.gitlabMRJSON(work, pr))
	case sub == "/approvals":
		var by []any
		for i, u := range pr.reviewers {
			if (i < len(pr.approved) && pr.approved[i] && u != me) || u == me && s.git.approved[iid] {
				by = append(by, map[string]any{"user": gitlabUser(u)})
			}
		}
		send(map[string]any{"approved": len(by) >= 2, "approvals_required": 2, "approvals_left": max(2-len(by), 0), "approved_by": append([]any{}, by...)})
	case sub == "/approve":
		s.git.approved[iid] = true
		send(map[string]any{})
	case sub == "/diffs":
		if q.Get("page") != "1" && q.Get("page") != "" {
			send([]any{})
			break
		}
		send(gitlabFiles(iid, 2))
	case sub == "/versions":
		vs := []any{map[string]any{"id": iid*10 + 2, "head_commit_sha": headSHA(iid, 2), "base_commit_sha": "b4se0000", "start_commit_sha": "b4se0000",
			"created_at": pr.updated.UTC().Format(time.RFC3339)}}
		if iid == 87 {
			vs = append(vs, map[string]any{"id": iid*10 + 1, "head_commit_sha": headSHA(iid, 1), "base_commit_sha": "b4se0000", "start_commit_sha": "b4se0000",
				"created_at": pr.updated.Add(-30 * time.Hour).UTC().Format(time.RFC3339)})
		}
		send(vs)
	case strings.HasPrefix(sub, "/versions/"):
		v := 2
		if strings.HasSuffix(sub, "1") {
			v = 1
		}
		send(map[string]any{"id": iid*10 + v, "head_commit_sha": headSHA(iid, v), "base_commit_sha": "b4se0000", "start_commit_sha": "b4se0000", "diffs": gitlabFiles(iid, v)})
	case sub == "/discussions" && r.Method == http.MethodGet:
		out := []any{}
		for _, t := range s.gitlabThreads(iid) {
			out = append(out, t.json(s.git.resolved[t.id]))
		}
		send(out)
	case strings.HasPrefix(sub, "/discussions/") && r.Method == http.MethodPut: // resolve, reopen
		res, _ := body["resolved"].(bool)
		s.git.resolved[strings.TrimPrefix(sub, "/discussions/")] = res
		send(map[string]any{})
	case strings.HasPrefix(sub, "/discussions") && r.Method == http.MethodPost: // a note, a reply
		text, _ := body["body"].(string)
		reply := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(sub, "/discussions"), "/"), "/notes")
		s.addGitLabNote(iid, text, reply, posOf(body))
		send(map[string]any{})
	case sub == "/draft_notes" && r.Method == http.MethodGet:
		out := []any{}
		for _, d := range s.git.drafts[iid] {
			dj := map[string]any{"id": d.id, "note": d.body, "discussion_id": d.replyTo}
			if d.pos != nil {
				dj["position"] = d.pos
			}
			out = append(out, dj)
		}
		send(out)
	case sub == "/draft_notes" && r.Method == http.MethodPost:
		s.git.seq++
		text, _ := body["note"].(string)
		reply, _ := body["in_reply_to_discussion_id"].(string)
		pos, _ := body["position"].(map[string]any)
		s.git.drafts[iid] = append(s.git.drafts[iid], gitlabDraft{id: s.git.seq, body: text, replyTo: reply, pos: pos})
		send(map[string]any{"id": s.git.seq})
	case sub == "/draft_notes/bulk_publish":
		for _, d := range s.git.drafts[iid] {
			s.addGitLabNote(iid, d.body, d.replyTo, d.pos)
		}
		if text, _ := body["note"].(string); text != "" {
			s.addGitLabNote(iid, text, "", nil)
		}
		s.git.drafts[iid] = nil
		send(map[string]any{})
	case strings.HasPrefix(sub, "/draft_notes/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(sub, "/draft_notes/"))
		ds := s.git.drafts[iid]
		i := slices.IndexFunc(ds, func(d gitlabDraft) bool { return d.id == id })
		switch {
		case i < 0:
		case r.Method == http.MethodDelete:
			s.git.drafts[iid] = slices.Delete(ds, i, i+1)
		case r.Method == http.MethodPut:
			ds[i].body, _ = body["note"].(string)
		}
		send(map[string]any{})
	default:
		send(map[string]any{"message": "404 Not found"})
	}
	return true
}

// addGitLabNote keeps a note the demo posted: a new thread, or a reply in
// replyTo's.
func (s *Server) addGitLabNote(iid int, text, replyTo string, pos map[string]any) {
	s.git.seq++
	t := gitlabThread{id: "n" + strconv.Itoa(s.git.seq), author: me, body: text, at: s.now}
	if replyTo != "" {
		s.git.replies[replyTo] = append(s.git.replies[replyTo], t)
		return
	}
	if pos != nil {
		t.path, _ = pos["new_path"].(string)
		t.newLine = intOf(pos["new_line"])
		t.oldLine = intOf(pos["old_line"])
	}
	s.git.notes[iid] = append(s.git.notes[iid], t)
}

func posOf(body map[string]any) map[string]any {
	p, _ := body["position"].(map[string]any)
	return p
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}
