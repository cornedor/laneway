// Package demo is a Jira served in-process for `laneway -demo`: a
// generated project to try the app on without a site or a token. Writes
// change it in memory and are gone when the app ends.
package demo

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	mu       sync.Mutex
	now      time.Time
	issues   map[string]*issue
	order    []string // board issues in rank order
	hidden   []string // closed sprints' issues
	sprints  []sprint
	versions []version
	seq      int
	links    []link
	linkSeq  int
	// Unhandled are the requests no route answered, for tests.
	Unhandled []string
}

// New generates the project, dated around now.
func New(now time.Time) *Server { return generate(now) }

// Start serves s on a loopback port; stop ends it.
func (s *Server) Start() (baseURL string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	return "http://" + ln.Addr().String(), func() { srv.Close() }, nil
}

var (
	issueRe      = regexp.MustCompile(`^/rest/api/3/issue/([A-Z]+-\d+)(/[a-z]+)?(/\d+)?$`)
	sprintIssues = regexp.MustCompile(`^/rest/agile/1\.0/(?:board/\d+/)?sprint/(\d+)/issue$`)
	sprintRe     = regexp.MustCompile(`^/rest/agile/1\.0/sprint/(\d+)$`)
	versionRe    = regexp.MustCompile(`^/rest/api/3/version/(\d+)$`)
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body map[string]any
	if raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	p, q := r.URL.Path, r.URL.Query()
	send := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	if r.Method == http.MethodPost && p == "/rest/api/3/search/jql" {
		jql, _ := body["jql"].(string)
		expand, _ := body["expand"].(string)
		send(map[string]any{"issues": s.list(s.search(jql), strings.Contains(expand, "changelog")), "isLast": true})
		return
	}
	if m := issueRe.FindStringSubmatch(p); m != nil {
		if iss := s.issues[m[1]]; iss != nil {
			if v, ok := s.issueRoute(r.Method, iss, m[2], strings.TrimPrefix(m[3], "/"), body, q); ok {
				if v == nil {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				send(v)
				return
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
			send(map[string]any{"errorMessages": []string{"Issue does not exist or you do not have permission to see it."}})
			return
		}
	}
	if m := sprintIssues.FindStringSubmatch(p); m != nil {
		id, _ := strconv.Atoi(m[1])
		if r.Method == http.MethodPost {
			s.moveTo(id, body)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		send(s.page(s.filter(s.board(func(i *issue) bool { return i.sprint == id }), q.Get("jql"))))
		return
	}
	switch {
	case r.Method == http.MethodPost && p == "/rest/api/3/issue":
		send(s.create(body))
		return
	case r.Method == http.MethodPost && p == "/rest/agile/1.0/backlog/issue":
		s.moveTo(0, body)
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodPut && versionRe.MatchString(p):
		s.release(versionRe.FindStringSubmatch(p)[1], body)
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodPut && p == "/rest/agile/1.0/issue/rank":
		s.rank(body)
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodPost && p == "/rest/api/3/issueLink":
		if !s.addLink(body) {
			w.WriteHeader(http.StatusBadRequest)
			send(map[string]any{"errorMessages": []string{"unknown link type or issue"}})
			return
		}
		w.WriteHeader(http.StatusCreated)
		return
	case r.Method == http.MethodDelete && strings.HasPrefix(p, "/rest/api/3/issueLink/"):
		id := strings.TrimPrefix(p, "/rest/api/3/issueLink/")
		s.links = slices.DeleteFunc(s.links, func(l link) bool { return l.id == id })
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method != http.MethodGet:
		s.Unhandled = append(s.Unhandled, r.Method+" "+p)
		w.WriteHeader(http.StatusNoContent) // any other write succeeds and changes nothing
		return
	}
	if v, ok := s.read(p, q); ok {
		send(v)
		return
	}
	s.Unhandled = append(s.Unhandled, r.Method+" "+p)
	w.WriteHeader(http.StatusNotFound)
	send(map[string]any{"errorMessages": []string{"the demo has no " + p}})
}

// read answers the GETs that are not about one issue.
func (s *Server) read(p string, q map[string][]string) (any, bool) {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	switch p {
	case "/rest/api/3/myself":
		return map[string]any{"accountId": me.id, "displayName": me.name, "emailAddress": "demo@example.com", "timeZone": "UTC"}, true
	case "/rest/api/3/serverInfo":
		return map[string]any{"deploymentType": "Cloud"}, true
	case "/rest/api/3/project/search":
		return map[string]any{"isLast": true, "total": 1, "values": []any{projectJSON()}}, true
	case "/rest/api/3/project/" + project:
		return projectJSON(), true
	case "/rest/api/3/field":
		return fieldsJSON(), true
	case "/rest/api/3/status":
		out := []any{}
		for _, st := range statuses {
			out = append(out, statusJSON(st))
		}
		return out, true
	case "/rest/api/3/priority":
		out := []any{}
		for i, n := range priorities {
			out = append(out, map[string]any{"id": strconv.Itoa(i + 1), "name": n})
		}
		return out, true
	case "/rest/api/3/filter/favourite":
		return []any{}, true
	case "/rest/api/3/issueLinkType":
		out := []any{}
		for i, t := range linkTypes {
			out = append(out, map[string]any{"id": strconv.Itoa(10000 + i), "name": t[0], "inward": t[1], "outward": t[2]})
		}
		return map[string]any{"issueLinkTypes": out}, true
	case "/rest/api/3/user/assignable/search", "/rest/api/3/user/search", "/rest/api/3/user/viewissue/search":
		out := []any{}
		for _, u := range users {
			if strings.Contains(strings.ToLower(u.name), strings.ToLower(get("query"))) {
				out = append(out, userJSON(&u))
			}
		}
		return out, true
	case "/rest/api/3/project/" + project + "/role":
		base := "/rest/api/3/project/" + project + "/role/"
		return map[string]any{"Administrators": base + "10002", "Developers": base + "10001"}, true
	case "/rest/api/3/project/" + project + "/version":
		return map[string]any{"values": s.versionsJSON(), "isLast": true}, true
	case "/rest/api/3/project/" + project + "/statuses":
		var sts []any
		for _, st := range statuses {
			sts = append(sts, statusJSON(st))
		}
		var out []any
		for name, id := range types {
			out = append(out, map[string]any{"id": id, "name": name, "subtask": name == "Sub-task", "statuses": sts})
		}
		return out, true
	case "/rest/api/3/issue/createmeta/" + project + "/issuetypes":
		var out []any
		for _, name := range []string{"Story", "Task", "Bug", "Epic"} {
			out = append(out, map[string]any{"id": types[name], "name": name, "subtask": false})
		}
		return map[string]any{"issueTypes": out, "values": out, "isLast": true}, true
	case "/rest/api/3/jql/autocompletedata":
		return jqlWordsJSON(), true
	case "/rest/api/3/jql/autocompletedata/suggestions":
		return map[string]any{"results": s.jqlValues(get("fieldName"), get("fieldValue"))}, true
	case "/rest/dev-status/latest/issue/summary", "/rest/dev-status/latest/issue/detail":
		return s.devStatus(p, get), true
	case "/rest/agile/1.0/board":
		return map[string]any{"isLast": true, "total": 1, "values": []any{map[string]any{"id": boardID, "name": "DEMO board", "type": "scrum"}}}, true
	}
	if strings.HasPrefix(p, "/rest/api/3/issue/createmeta/"+project+"/issuetypes/") {
		return map[string]any{"fields": []any{
			map[string]any{"fieldId": "summary", "name": "Summary", "required": true, "schema": map[string]any{"type": "string"}},
			map[string]any{"fieldId": "description", "name": "Description", "required": false, "schema": map[string]any{"type": "string"}},
		}, "values": []any{}, "isLast": true}, true
	}
	b := "/rest/agile/1.0/board/" + strconv.Itoa(boardID)
	switch p {
	case b + "/configuration":
		var cols []any
		for i, st := range statuses {
			if st == qa { // shares the In Review column
				continue
			}
			sts := []any{map[string]any{"id": st.id}}
			if st == review {
				sts = append(sts, map[string]any{"id": qa.id})
			}
			col := map[string]any{"name": st.name, "statuses": sts}
			if i == 1 {
				col["max"] = 4
			}
			cols = append(cols, col)
		}
		return map[string]any{"id": boardID, "name": "DEMO board", "columnConfig": map[string]any{"columns": cols},
			"estimation": map[string]any{"type": "field", "field": map[string]any{"fieldId": pointsField, "displayName": "Story Points"}}}, true
	case b + "/quickfilter":
		qf := []any{
			map[string]any{"id": 1, "name": "Only my issues", "jql": "assignee = currentUser()"},
			map[string]any{"id": 2, "name": "Bugs", "jql": "type = Bug"},
			map[string]any{"id": 3, "name": "Flagged", "jql": "flagged is not EMPTY"},
		}
		return map[string]any{"values": qf, "total": len(qf), "isLast": true}, true
	case b + "/sprint":
		var out []any
		for _, sp := range s.sprints {
			if strings.Contains(get("state"), sp.state) || get("state") == "" {
				out = append(out, sprintJSON(sp))
			}
		}
		return map[string]any{"values": out, "isLast": true}, true
	case b + "/backlog":
		return s.page(s.filter(s.board(func(i *issue) bool { return i.sprint == 0 && i.typ != "Sub-task" }), get("jql"))), true
	case b + "/issue":
		return s.page(s.filter(s.board(func(*issue) bool { return true }), get("jql"))), true
	case b + "/features":
		return map[string]any{"features": []any{}}, true
	case b + "/epic":
		return map[string]any{"values": []any{}, "isLast": true}, true
	}
	if m := sprintRe.FindStringSubmatch(p); m != nil {
		id, _ := strconv.Atoi(m[1])
		for _, sp := range s.sprints {
			if sp.id == id {
				return sprintJSON(sp), true
			}
		}
	}
	return nil, false
}

// issueRoute answers /issue/KEY[/sub[/id]]; nil with ok is a 204.
func (s *Server) issueRoute(method string, iss *issue, sub, id string, body map[string]any, q map[string][]string) (any, bool) {
	switch method + " " + sub {
	case "GET ":
		out := s.issueJSON(iss, true)
		if strings.Contains(firstOf(q["expand"]), "editmeta") {
			out["editmeta"] = editMetaJSON()
		}
		return out, true
	case "PUT ":
		s.edit(iss, body)
		return nil, true
	case "PUT /assignee":
		s.edit(iss, map[string]any{"fields": map[string]any{"assignee": body}})
		return nil, true
	case "GET /transitions":
		var out []any
		for _, st := range statuses {
			out = append(out, map[string]any{"id": "1" + st.id, "name": st.name, "to": statusJSON(st), "fields": map[string]any{}})
		}
		return map[string]any{"transitions": out}, true
	case "POST /transitions":
		if tr, _ := body["transition"].(map[string]any); tr != nil {
			tid, _ := tr["id"].(string)
			for _, st := range statuses {
				if "1"+st.id == tid && st != iss.status {
					iss.changes = append(iss.changes, change{author: me, at: time.Now(), field: "status", from: iss.status.name, to: st.name, fromID: iss.status.id, toID: st.id})
					iss.status, iss.updated = st, time.Now()
					if st == done {
						iss.resolved = time.Now()
					} else {
						iss.resolved = time.Time{}
					}
				}
			}
		}
		return nil, true
	case "GET /comment":
		out := []any{}
		for _, c := range iss.comments {
			out = append(out, commentJSON(c))
		}
		if strings.HasPrefix(firstOf(q["orderBy"]), "-") {
			slices.Reverse(out)
		}
		return map[string]any{"comments": out, "total": len(out), "maxResults": len(out), "startAt": 0}, true
	case "POST /comment":
		s.seq++
		c := comment{id: strconv.Itoa(s.seq), author: me, body: adfText(body["body"]), created: time.Now()}
		iss.comments = append(iss.comments, c)
		iss.updated = time.Now()
		return commentJSON(c), true
	case "PUT /comment":
		for i, c := range iss.comments {
			if c.id == id {
				iss.comments[i].body = adfText(body["body"])
				return commentJSON(iss.comments[i]), true
			}
		}
		return nil, true
	case "DELETE /comment":
		iss.comments = slices.DeleteFunc(iss.comments, func(c comment) bool { return c.id == id })
		return nil, true
	case "GET /worklog":
		out := []any{}
		for _, wl := range iss.worklogs {
			out = append(out, worklogJSON(wl))
		}
		return map[string]any{"worklogs": out, "total": len(out), "maxResults": len(out), "startAt": 0}, true
	case "POST /worklog":
		secs, _ := body["timeSpentSeconds"].(float64)
		started := time.Now()
		if v, _ := body["started"].(string); v != "" {
			if t, err := time.Parse(jiraTime, v); err == nil {
				started = t
			}
		}
		s.seq++
		wl := worklog{id: strconv.Itoa(s.seq), author: me, started: started, seconds: int(secs), comment: adfText(body["comment"])}
		iss.worklogs = append(iss.worklogs, wl)
		return worklogJSON(wl), true
	case "DELETE /worklog":
		iss.worklogs = slices.DeleteFunc(iss.worklogs, func(w worklog) bool { return w.id == id })
		return nil, true
	case "GET /changelog":
		out := []any{}
		for _, c := range iss.changes {
			out = append(out, changeJSON(c))
		}
		return map[string]any{"values": out, "total": len(out), "startAt": 0, "isLast": true}, true
	case "GET /editmeta":
		return map[string]any{"fields": map[string]any{}}, true
	case "GET /watchers":
		return map[string]any{"isWatching": false, "watchCount": 0, "watchers": []any{}}, true
	case "GET /votes":
		return map[string]any{"votes": 0, "hasVoted": false}, true
	case "GET /remotelink":
		return []any{}, true
	}
	return nil, false
}

func firstOf(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// edit applies a PUT's fields: summary, description, assignee, priority,
// points, labels, due date, flag.
func (s *Server) edit(iss *issue, body map[string]any) {
	f, _ := body["fields"].(map[string]any)
	for k, v := range f {
		switch k {
		case "summary":
			iss.summary, _ = v.(string)
		case "description":
			iss.description = adfText(v)
		case "assignee":
			iss.assignee = nil
			if m, _ := v.(map[string]any); m != nil {
				for i := range users {
					if users[i].id == m["accountId"] {
						iss.assignee = &users[i]
					}
				}
			}
		case "priority":
			if m, _ := v.(map[string]any); m != nil {
				if n, ok := m["name"].(string); ok {
					iss.priority = n
				} else if id, ok := m["id"].(string); ok {
					if i, _ := strconv.Atoi(id); i >= 1 && i <= len(priorities) {
						iss.priority = priorities[i-1]
					}
				}
			}
		case pointsField:
			iss.points, _ = v.(float64)
		case "labels":
			iss.labels = nil
			for _, l := range asSlice(v) {
				if s, ok := l.(string); ok {
					iss.labels = append(iss.labels, s)
				}
			}
		case "duedate":
			iss.due, _ = v.(string)
		case "components":
			iss.components = nil
			for _, c := range asSlice(v) {
				m, _ := c.(map[string]any)
				if i := slices.IndexFunc(components, func(n string) bool { return optionID(components, n) == m["id"] }); i >= 0 {
					iss.components = append(iss.components, components[i])
				}
			}
		case teamField:
			iss.team = ""
			if m, _ := v.(map[string]any); m != nil {
				if i := slices.IndexFunc(teams, func(n string) bool { return optionID(teams, n) == m["id"] }); i >= 0 {
					iss.team = teams[i]
				}
			}
		case "parent":
			iss.parent = ""
			if m, _ := v.(map[string]any); m != nil {
				iss.parent, _ = m["key"].(string)
			}
		case flagField:
			iss.flagged = len(asSlice(v)) > 0
		}
	}
	// Jira's other shape: update: {labels: [{add: x}], …}.
	u, _ := body["update"].(map[string]any)
	for _, op := range asSlice(u["labels"]) {
		m, _ := op.(map[string]any)
		if a, ok := m["add"].(string); ok && !slices.Contains(iss.labels, a) {
			iss.labels = append(iss.labels, a)
		}
		if r, ok := m["remove"].(string); ok {
			iss.labels = slices.DeleteFunc(iss.labels, func(l string) bool { return l == r })
		}
	}
	iss.updated = time.Now()
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// create adds an issue from a POST: its summary, type, parent and
// description; a new one goes to the backlog.
func (s *Server) create(body map[string]any) any {
	f, _ := body["fields"].(map[string]any)
	key := fmt.Sprintf("%s-%d", project, len(s.order)+1)
	for s.issues[key] != nil {
		key += "0"
	}
	iss := &issue{key: key, typ: "Task", status: todo, priority: "Medium", reporter: me, created: time.Now(), updated: time.Now()}
	iss.summary, _ = f["summary"].(string)
	iss.description = adfText(f["description"])
	if t, _ := f["issuetype"].(map[string]any); t != nil {
		for name, id := range types {
			if t["id"] == id || t["name"] == name {
				iss.typ = name
			}
		}
	}
	if p, _ := f["parent"].(map[string]any); p != nil {
		iss.parent, _ = p["key"].(string)
	}
	s.issues[key] = iss
	s.order = append(s.order, key)
	return map[string]any{"id": numID(key), "key": key}
}

// moveTo puts the body's issues in sprint id, 0 the backlog.
func (s *Server) moveTo(id int, body map[string]any) {
	for _, k := range asSlice(body["issues"]) {
		if iss := s.issues[fmt.Sprint(k)]; iss != nil {
			iss.sprint = id
		}
	}
}

// rank moves the body's issues before rankBeforeIssue, or after
// rankAfterIssue.
func (s *Server) rank(body map[string]any) {
	var keys []string
	for _, k := range asSlice(body["issues"]) {
		keys = append(keys, fmt.Sprint(k))
	}
	rest := slices.DeleteFunc(slices.Clone(s.order), func(k string) bool { return slices.Contains(keys, k) })
	at := len(rest)
	if b, ok := body["rankBeforeIssue"].(string); ok {
		if i := slices.Index(rest, b); i >= 0 {
			at = i
		}
	} else if a, ok := body["rankAfterIssue"].(string); ok {
		if i := slices.Index(rest, a); i >= 0 {
			at = i + 1
		}
	}
	s.order = slices.Insert(rest, at, keys...)
}

// board is the ranked board issues keep says yes to; epics are not on it.
func (s *Server) board(keep func(*issue) bool) []*issue {
	var out []*issue
	for _, k := range s.order {
		if iss := s.issues[k]; iss.typ != "Epic" && keep(iss) {
			out = append(out, iss)
		}
	}
	return out
}

func (s *Server) page(issues []*issue) map[string]any {
	return map[string]any{"startAt": 0, "maxResults": len(issues), "total": len(issues), "issues": s.list(issues, false), "isLast": true}
}

func (s *Server) list(issues []*issue, changelog bool) []any {
	out := []any{}
	for _, iss := range issues {
		j := s.issueJSON(iss, false)
		if changelog {
			var hs []any
			for _, c := range iss.changes {
				hs = append(hs, changeJSON(c))
			}
			j["changelog"] = map[string]any{"histories": hs}
		}
		out = append(out, j)
	}
	return out
}

// linkTypes are name, inward, outward.
var linkTypes = [][3]string{
	{"Blocks", "is blocked by", "blocks"},
	{"Relates", "relates to", "relates to"},
	{"Duplicate", "is duplicated by", "duplicates"},
}

// link is one issue link: from is the inward end ("from blocks to").
type link struct{ id, typ, from, to string }

func (s *Server) addLink(body map[string]any) bool {
	name := func(k, f string) string {
		m, _ := body[k].(map[string]any)
		v, _ := m[f].(string)
		return v
	}
	typ, from, to := name("type", "name"), name("inwardIssue", "key"), name("outwardIssue", "key")
	if s.issues[from] == nil || s.issues[to] == nil || from == to || !slices.ContainsFunc(linkTypes, func(t [3]string) bool { return t[0] == typ }) {
		return false
	}
	s.linkSeq++
	s.links = append(s.links, link{strconv.Itoa(20000 + s.linkSeq), typ, from, to})
	return true
}

// linksJSON is the issuelinks field of key: each link names the other end.
func (s *Server) linksJSON(key string) []any {
	out := []any{}
	for _, l := range s.links {
		if l.from != key && l.to != key {
			continue
		}
		var t [3]string
		for _, lt := range linkTypes {
			if lt[0] == l.typ {
				t = lt
			}
		}
		e := map[string]any{"id": l.id, "type": map[string]any{"name": t[0], "inward": t[1], "outward": t[2]}}
		other, side := l.to, "outwardIssue"
		if l.to == key {
			other, side = l.from, "inwardIssue"
		}
		if o := s.issues[other]; o != nil {
			e[side] = map[string]any{"key": o.key, "fields": map[string]any{"summary": o.summary, "status": statusJSON(o.status)}}
			out = append(out, e)
		}
	}
	return out
}
