package ui

import (
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestDiffDiscussions: a general thread and an outdated one show under
// Discussions, the outdated one marked; c on one replies into the pending
// review, x drops it, S submits with a verdict and summary, A approves.
func TestDiffDiscussions(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
			mu.Unlock()
			w.Write([]byte(`{}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1 +1 @@\n-x\n+y\n"}]`))
		case strings.HasSuffix(r.URL.Path, "/discussions"):
			w.Write([]byte(`[{"id": "g1", "notes": [{"id": 1, "body": "Looks good overall", "author": {"name": "Grace"}, "resolvable": true}]},
				{"id": "o1", "notes": [{"id": 2, "body": "old remark", "author": {"name": "Ada"}, "resolvable": true,
					"position": {"position_type": "text", "new_path": "a.go", "new_line": 9, "head_sha": "older"}}]}]`))
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/draft_notes"):
			mu.Lock()
			n := len(writes)
			mu.Unlock()
			if n == 0 {
				w.Write([]byte(`[]`))
			} else { // after the reply: it is pending
				w.Write([]byte(`[{"id": 5, "note": "Thanks", "discussion_id": "g1"}]`))
			}
		default:
			w.Write([]byte(`{"iid": 7, "title": "T", "state": "opened", "diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}}`))
		}
	}))
	defer srv.Close()
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	link := srv.URL + "/g/p/-/merge_requests/7"
	c, r, _ := m.gitlabMR(link)
	out, _ := m.Update(m.showMR(c, r, link, "T")())
	m = out.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Threads    2 open") {
		t.Errorf("panel: no thread counts:\n%s", view)
	}
	out, cmd := m.handleKey(keyMsg(t, "d"))
	out, _ = out.(Model).Update(cmd())
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Discussions", "Looks good overall", "outdated · a.go:9", "old remark"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	d := m.diff
	for i, row := range d.rows { // the cursor onto the general thread
		if row.kind == diffRowNote && row.noteHead && d.threads[row.thread].ID == "g1" {
			d.setPos(d.visPos[i])
		}
	}
	out, _ = m.handleKey(keyMsg(t, "c"))
	m = out.(Model)
	if !m.diffNoteActive() || m.diff.note.replyTo != "g1" {
		t.Fatalf("c: not a reply to g1: %+v", m.diff.note)
	}
	m.diff.note.input.SetValue("Thanks")
	m = pressAll(t, m, keyMsg(t, "enter"))
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "✎ you (pending)") || !strings.Contains(view, "S submit 1 pending note") {
		t.Errorf("the reply is not pending:\n%s", view)
	}
	for i, row := range m.diff.rows { // onto the pending reply: x drops it
		if row.draft > 0 {
			m.diff.setPos(m.diff.visPos[i])
			break
		}
	}
	out, _ = m.handleKey(keyMsg(t, "E"))
	m = out.(Model)
	if !m.diffNoteActive() || m.diff.note.edit != 5 || m.diff.note.input.Value() != "Thanks" {
		t.Fatalf("E: not the pending note: %+v", m.diff.note)
	}
	m.diff.note.input.SetValue("Thanks a lot")
	m = pressAll(t, m, keyMsg(t, "enter"))
	for i, row := range m.diff.rows {
		if row.draft > 0 {
			m.diff.setPos(m.diff.visPos[i])
			break
		}
	}
	m = pressAll(t, m, keyMsg(t, "x"))
	out, _ = m.handleKey(keyMsg(t, "S"))
	out, _ = out.(Model).handleKey(keyMsg(t, "down")) // Approve
	out, _ = out.(Model).handleKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.diffNoteActive() || m.diff.note.submit != "approve" {
		t.Fatalf("S: no summary for approve: %+v", m.diff.note)
	}
	m.diff.note.input.SetValue("LGTM")
	m = pressAll(t, m, keyMsg(t, "enter"))
	pressAll(t, m, keyMsg(t, "A"))
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"POST /api/v4/projects/g/p/merge_requests/7/draft_notes",
		"PUT /api/v4/projects/g/p/merge_requests/7/draft_notes/5",
		"DELETE /api/v4/projects/g/p/merge_requests/7/draft_notes/5",
		"POST /api/v4/projects/g/p/merge_requests/7/draft_notes/bulk_publish",
		"POST /api/v4/projects/g/p/merge_requests/7/approve",
		"POST /api/v4/projects/g/p/merge_requests/7/approve",
	}
	if len(writes) != len(want) {
		t.Fatalf("writes:\n%s", strings.Join(writes, "\n"))
	}
	for i, w := range want {
		if !strings.HasPrefix(writes[i], w+" ") {
			t.Errorf("write %d: %s, want %s", i, writes[i], w)
		}
	}
	if !strings.Contains(writes[0], `"in_reply_to_discussion_id":"g1"`) || !strings.Contains(writes[0], "Thanks") ||
		!strings.Contains(writes[1], `"note":"Thanks a lot"`) ||
		!strings.Contains(writes[3], `"note":"LGTM"`) || !strings.Contains(writes[3], `"reviewer_state":"reviewed"`) {
		t.Errorf("bodies:\n%s", strings.Join(writes, "\n"))
	}
}

// pressAll presses key and runs what it starts, and what that starts, to the
// end (a batch's commands one by one).
func pressAll(t *testing.T, m Model, key tea.KeyPressMsg) Model {
	t.Helper()
	out, cmd := m.handleKey(key)
	m = out.(Model)
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case nil:
		default:
			out, next := m.Update(msg)
			m = out.(Model)
			queue = append(queue, next)
		}
	}
	return m
}

// TestDiffRangeSuggestion: V and moving mark lines; c notes them with a
// line_range GitLab names by line code; s opens a suggestion of the new
// side's lines.
func TestDiffRangeSuggestion(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			writes = append(writes, string(b))
			mu.Unlock()
			w.Write([]byte(`{}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1,2 +1,3 @@\n keep\n-old\n+new1\n+\tnew2\n"}]`))
		case strings.HasSuffix(r.URL.Path, "/discussions"), strings.HasSuffix(r.URL.Path, "/draft_notes"), strings.HasSuffix(r.URL.Path, "/versions"):
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`{"iid": 7, "title": "T", "state": "opened", "diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}}`))
		}
	}))
	defer srv.Close()
	m := loadedJiraModel(t).WithGitLab(gitlab.NewSites([]gitlab.Config{{BaseURL: srv.URL, Token: "tok"}}))
	link := srv.URL + "/g/p/-/merge_requests/7"
	c, r, _ := m.gitlabMR(link)
	out, _ := m.Update(m.showMR(c, r, link, "T")())
	m = pressAll(t, out.(Model), keyMsg(t, "d"))
	at := func(text string) {
		for i, row := range m.diff.rows {
			if row.raw == text && row.kind.commentable() {
				m.diff.setPos(m.diff.visPos[i])
				return
			}
		}
		t.Fatalf("no row %q", text)
	}
	at("new1")
	out, _ = m.handleKey(keyMsg(t, "V"))
	m = out.(Model)
	at("\tnew2")
	out, _ = m.handleKey(keyMsg(t, "s"))
	m = out.(Model)
	if v := m.diff.note.input.Value(); v != "```suggestion:-1+0\nnew1\n⇥new2\n```" || m.diff.note.lines == nil {
		t.Fatalf("suggestion %q, range %+v", v, m.diff.note.lines)
	}
	m = pressAll(t, m, keyMsg(t, "enter"))
	mu.Lock()
	defer mu.Unlock()
	code := fmt.Sprintf("%x", sha1.Sum([]byte("a.go")))
	if len(writes) != 1 || !strings.Contains(writes[0], `"line_code":"`+code+`_3_2"`) || !strings.Contains(writes[0], `"line_code":"`+code+`_3_3"`) ||
		!strings.Contains(writes[0], `"type":"new"`) || !strings.Contains(writes[0], `"new_line":3`) || !strings.Contains(writes[0], `suggestion:-1+0\nnew1\n\tnew2`) {
		t.Errorf("writes:\n%s", strings.Join(writes, "\n"))
	}
}
