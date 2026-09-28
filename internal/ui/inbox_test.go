package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// TestInbox: I lists the entries, marks them read, and enter opens one.
func TestInbox(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case "/rest/api/3/search/jql":
			io.WriteString(w, `{"issues":[{"key":"ABC-2","fields":{"summary":"Second"}}]}`)
		case "/rest/api/3/issue/ABC-2/changelog":
			io.WriteString(w, `{"total":1,"values":[{"author":{"accountId":"bob","displayName":"Bob"},
			  "created":"`+time.Now().Add(-time.Minute).Format("2006-01-02T15:04:05.000-0700")+`","items":[{"field":"status","fromString":"To Do","toString":"Done"}]}]}`)
		default:
			io.WriteString(w, `{"comments":[]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, cmd := m.handleJiraKey(keyMsg(t, "I"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickInbox {
		t.Fatal("I should open the inbox")
	}
	out, _ = m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Inbox — 1 change since") || !strings.Contains(view, "Bob  ABC-2 Second — status: To Do → Done") {
		t.Errorf("inbox not drawn:\n%s", view)
	}
	v, _, _ := m.store.GetMeta(inboxMeta)
	if sec, _ := strconv.ParseInt(v, 10, 64); time.Since(time.Unix(sec, 0)) > time.Minute {
		t.Errorf("seen = %q, want now", v)
	}
	if since := m.inboxSince(time.Now()); time.Since(since) > time.Minute {
		t.Errorf("next since = %v", since)
	}
	out, _ = m.applyJiraPick()
	if m = out.(Model); !m.refOpen || m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Error("enter should open the issue")
	}
}

// TestInboxBadge: the count shows in the header until the inbox has
// loaded; a failed load keeps it.
func TestInboxBadge(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.Update(inboxCountMsg{3})
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "✉ 3 I") {
		t.Fatal("badge not in the header")
	}
	m.openInbox()
	p := m.jiraPicker
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: p.gen, seq: p.fetchSeq, kind: jiraPickInbox, err: fmt.Errorf("down")})
	if m = out.(Model); m.inboxBadge() == "" {
		t.Error("a failed load cleared the badge")
	}
	out, _ = m.handleJiraPickerLoaded(jiraPickerLoadedMsg{gen: p.gen, seq: p.fetchSeq, kind: jiraPickInbox})
	if m = out.(Model); m.inboxBadge() != "" {
		t.Error("a loaded inbox should clear the badge")
	}
}

// TestMentionNotify: a mention after the app started notifies once; older
// ones and repeats stay quiet.
func TestMentionNotify(t *testing.T) {
	m := jiraTabModel(t)
	m.started = time.Now().Add(-time.Hour)
	old := jira.InboxEntry{Key: "ABC-1", Who: "Ann", Mention: true, When: m.started.Add(-time.Minute)}
	fresh := jira.InboxEntry{Key: "ABC-2", Who: "Bob", Mention: true, When: time.Now()}
	comment := jira.InboxEntry{Key: "ABC-3", When: time.Now()}
	out, cmd := m.handleInboxMentions(inboxMentionsMsg{[]jira.InboxEntry{old, fresh, comment}})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("a fresh mention should notify")
	}
	if _, cmd = m.handleInboxMentions(inboxMentionsMsg{[]jira.InboxEntry{old, fresh}}); cmd != nil {
		t.Error("the same mention should not notify twice")
	}
	if _, cmd := m.handleInboxCount(inboxCountMsg{0}); cmd != nil {
		t.Error("a count that did not rise should not read the inbox")
	}
}

func TestInboxOptions(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{InboxEvery: "off", InboxLookback: "72h", InboxIssues: 50})
	if o.inboxEvery != 0 || o.inboxLookback != 72*time.Hour || o.inboxIssues != 50 || len(warn) != 0 {
		t.Fatalf("options %v %v %d %v", o.inboxEvery, o.inboxLookback, o.inboxIssues, warn)
	}
	m := jiraTabModel(t)
	m.store = nil
	m.opts = o
	now := time.Now()
	if got := m.inboxSince(now); !got.Equal(now.Add(-72 * time.Hour)) {
		t.Errorf("since = %v", got)
	}
	if m.inboxTick() != nil {
		t.Error("ticks with inbox_every off")
	}
	if _, warn = optionsFrom(config.UIConfig{InboxIssues: 999}); len(warn) != 1 {
		t.Errorf("cap unchecked: %v", warn)
	}
}

func TestInboxCount(t *testing.T) {
	for _, c := range []struct {
		changes, issues int
		want            string
	}{{0, 0, "0 changes on 0 issues"}, {1, 1, "1 change"}, {4, 1, "4 changes on 1 issue"}, {11, 3, "11 changes on 3 issues"}} {
		if got := inboxCount(c.changes, c.issues); got != c.want {
			t.Errorf("inboxCount(%d, %d) = %q, want %q", c.changes, c.issues, got, c.want)
		}
	}
}

// TestInboxPrevious: an inbox opened and closed by accident comes back from
// the next one's last row, without moving the read marks.
func TestInboxPrevious(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case "/rest/api/3/search/jql":
			io.WriteString(w, `{"issues":[{"key":"ABC-2","fields":{"summary":"Second"}}]}`)
		case "/rest/api/3/issue/ABC-2/changelog":
			io.WriteString(w, `{"total":1,"values":[{"author":{"accountId":"bob","displayName":"Bob"},
			  "created":"`+time.Now().Add(-time.Hour).Format("2006-01-02T15:04:05.000-0700")+`","items":[{"field":"status","fromString":"To Do","toString":"Done"}]}]}`)
		default:
			io.WriteString(w, `{"comments":[]}`)
		}
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	open := func(cmd tea.Cmd) {
		out, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
		m = out.(Model)
	}
	open(m.openInbox()) // read by accident, closed at once
	m.closeJiraPicker()
	open(m.openInbox())
	items := m.jiraPicker.items
	if len(items) != 2 || items[0].label != "nothing new · all caught up ✓" || items[1].id != inboxPrevID {
		t.Fatalf("second open = %+v", items)
	}
	seen, _, _ := m.store.GetMeta(inboxMeta)
	m.jiraPicker.idx = 1
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	open(cmd)
	if items := m.jiraPicker.items; len(items) != 1 || !strings.Contains(items[0].label, "Bob  ABC-2 Second — status: To Do → Done") {
		t.Errorf("the inbox before = %+v", m.jiraPicker.items)
	}
	if again, _, _ := m.store.GetMeta(inboxMeta); again != seen {
		t.Errorf("the marks moved: %s → %s", seen, again)
	}
}
