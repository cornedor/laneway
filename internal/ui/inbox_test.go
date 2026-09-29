package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

// fakeInbox is a Jira whose inbox holds a status change by Bob on each
// issue, a minute (or its age) ago; reads counts the changelog reads.
type fakeInbox struct {
	mu      sync.Mutex
	issues  []string // "KEY Summary"
	age     map[string]time.Duration
	comment map[string]string // a comment by Ann, "@" first to mention you
	updated time.Time
	reads   int
}

func (f *fakeInbox) client(t *testing.T) *jira.Client {
	jt := func(t time.Time) string { return t.Format("2006-01-02T15:04:05.000-0700") }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		key := strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/"), "/")[0]
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"me"}`)
		case r.URL.Path == "/rest/api/3/search/jql":
			var rows []string
			for _, is := range f.issues {
				k, sum, _ := strings.Cut(is, " ")
				rows = append(rows, fmt.Sprintf(`{"key":%q,"fields":{"summary":%q,"status":{"name":"In Progress"},"updated":%q}}`, k, sum, jt(f.updated)))
			}
			io.WriteString(w, `{"issues":[`+strings.Join(rows, ",")+`]}`)
		case strings.HasSuffix(r.URL.Path, "/changelog"):
			f.reads++
			age := f.age[key]
			if age == 0 {
				age = time.Minute
			}
			io.WriteString(w, `{"total":1,"values":[{"author":{"accountId":"bob","displayName":"Bob"},
			  "created":"`+jt(time.Now().Add(-age))+`","items":[{"field":"status","fromString":"To Do","toString":"Done"}]}]}`)
		case strings.HasSuffix(r.URL.Path, "/comment"):
			text, ok := f.comment[key]
			if !ok {
				io.WriteString(w, `{"comments":[]}`)
				return
			}
			node := fmt.Sprintf(`{"type":"text","text":%q}`, text)
			if rest, ok := strings.CutPrefix(text, "@"); ok {
				node = fmt.Sprintf(`{"type":"mention","attrs":{"id":"me","text":"@Me"}},{"type":"text","text":%q}`, " "+rest)
			}
			io.WriteString(w, `{"comments":[{"id":"9","author":{"accountId":"ann","displayName":"Ann"},"created":"`+jt(time.Now().Add(-30*time.Second))+`",
			  "body":{"type":"doc","content":[{"type":"paragraph","content":[`+node+`]}]}}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
}

// inboxModel is the board with f's inbox open and synced.
func inboxModel(t *testing.T, f *fakeInbox) Model {
	t.Helper()
	m := jiraTabModel(t)
	if f.updated.IsZero() {
		f.updated = time.Now().Add(-time.Minute)
	}
	m.jiraClient = f.client(t)
	out, cmd := m.handleJiraKey(keyMsg(t, "I"))
	m = out.(Model)
	if m.jiraTab.inbox == nil {
		t.Fatal("I should open the inbox")
	}
	return syncInbox(t, m, cmd)
}

func syncInbox(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("no sync")
	}
	out, _ := m.Update(cmd())
	return out.(Model)
}

func inboxKey(t *testing.T, m Model, k string) Model {
	t.Helper()
	out, _ := m.handleInboxKey(keyMsg(t, k))
	return out.(Model)
}

// TestInboxReadsOneThread: the inbox lists a thread per issue, and shows
// the cursor's in full; only that one is read, the rest stay unread.
func TestInboxReadsOneThread(t *testing.T) {
	f := &fakeInbox{issues: []string{"ABC-1 First", "ABC-2 Second", "ABC-3 Third"}, comment: map[string]string{"ABC-2": "have a look"}}
	m := inboxModel(t, f)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Inbox 3 · Mentions 0 · All 3", "ABC-1 First", "ABC-2 Second", "ABC-3 Third", "Bob: status: To Do → Done", "In Progress · unassigned"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	if m.inboxUnread != 2 {
		t.Errorf("unread %d, want 2: only the thread shown is read", m.inboxUnread)
	}
	if !strings.Contains(view, "● Ann · ") || !strings.Contains(view, "have a look") {
		t.Errorf("the newest thread's comment not shown in full:\n%s", view)
	}
	m = inboxKey(t, m, "j")
	if m.inboxUnread != 1 {
		t.Errorf("unread %d after moving on, want 1", m.inboxUnread)
	}
	m = inboxKey(t, m, "esc")
	cmd := m.openInbox()
	if m.jiraTab.inbox == nil || len(m.jiraTab.inbox.rows) != 3 {
		t.Fatal("the threads should open at once, before the sync")
	}
	m = syncInbox(t, m, cmd)
	if m.inboxUnread != 0 {
		t.Errorf("unread %d, want 0", m.inboxUnread)
	}
}

// TestInboxDone: e takes a thread off the list until something new
// happens on it; All still shows it, marked.
func TestInboxDone(t *testing.T) {
	f := &fakeInbox{issues: []string{"ABC-1 First", "ABC-2 Second"}}
	m := inboxModel(t, f)
	m = inboxKey(t, m, "e")
	s := m.jiraTab.inbox
	if len(s.rows) != 1 || s.rows[0] != "/ABC-2" {
		t.Fatalf("rows after done = %v", s.rows)
	}
	m = inboxKey(t, m, "tab")
	m = inboxKey(t, m, "tab")
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "✓") || len(m.jiraTab.inbox.rows) != 2 {
		t.Errorf("All should show the done one:\n%s", view)
	}
	f.mu.Lock()
	f.updated = time.Now()
	f.age = map[string]time.Duration{"ABC-1": time.Second}
	f.mu.Unlock()
	m = inboxKey(t, m, "tab")
	m = syncInbox(t, m, m.syncInbox())
	if rows := m.jiraTab.inbox.rows; len(rows) != 2 {
		t.Errorf("news on a done thread should bring it back: %v", rows)
	}
}

// TestInboxSnoozeAndUnread: s hides a thread till the next workday, u
// makes one unread again.
func TestInboxSnoozeAndUnread(t *testing.T) {
	f := &fakeInbox{issues: []string{"ABC-1 First", "ABC-2 Second"}}
	m := inboxModel(t, f)
	m = inboxKey(t, m, "s")
	if len(m.jiraTab.inbox.rows) != 1 || !strings.Contains(m.status, "snoozed till") {
		t.Fatalf("rows %v, status %q", m.jiraTab.inbox.rows, m.status)
	}
	if mk := m.inboxMark("/ABC-1"); time.UnixMilli(mk.Snooze).Before(time.Now()) {
		t.Errorf("snoozed till %v", time.UnixMilli(mk.Snooze))
	}
	if m.inboxUnread != 0 {
		t.Errorf("unread %d: ABC-2 is shown, ABC-1 snoozed", m.inboxUnread)
	}
	m = inboxKey(t, m, "u")
	if m.inboxUnread != 1 {
		t.Errorf("u: unread %d, want 1", m.inboxUnread)
	}
}

// TestInboxSyncReadsOnlyUpdated: a sync reads an issue's news again only
// when the issue was updated since.
func TestInboxSyncReadsOnlyUpdated(t *testing.T) {
	f := &fakeInbox{issues: []string{"ABC-1 First", "ABC-2 Second"}}
	m := inboxModel(t, f)
	m = syncInbox(t, m, m.syncInbox())
	if f.reads != 2 {
		t.Errorf("changelog read %d times, want 2", f.reads)
	}
	f.mu.Lock()
	f.updated = time.Now()
	f.mu.Unlock()
	syncInbox(t, m, m.syncInbox())
	if f.reads != 4 {
		t.Errorf("changelog read %d times after an update, want 4", f.reads)
	}
}

// TestInboxMarksKept: the marks outlive the app, in the state file.
func TestInboxMarksKept(t *testing.T) {
	f := &fakeInbox{issues: []string{"ABC-1 First"}}
	m := inboxModel(t, f)
	m = inboxKey(t, m, "e")
	again := jiraTabModel(t)
	again.store = m.store
	if mk := again.inboxMark("/ABC-1"); mk.Done == 0 || mk.Read == 0 {
		t.Errorf("marks not kept: %+v", mk)
	}
}

// TestInboxMentions: a mention sorts first, shows under Mentions, and R
// replies to it.
func TestInboxMentions(t *testing.T) {
	f := &fakeInbox{issues: []string{"ABC-1 First", "ABC-2 Second"}, comment: map[string]string{"ABC-2": "@ look"}, age: map[string]time.Duration{"ABC-1": time.Second}}
	m := inboxModel(t, f)
	if s := m.jiraTab.inbox; s.rows[0] != "/ABC-2" {
		t.Errorf("the mention should come first: %v", s.rows)
	}
	m = inboxKey(t, m, "tab")
	if s := m.jiraTab.inbox; len(s.rows) != 1 || s.rows[0] != "/ABC-2" {
		t.Errorf("Mentions = %v", s.rows)
	}
	m = inboxKey(t, m, "R")
	if !m.jiraCommentActive || m.jiraCommentKey != "ABC-2" || m.jiraCommentReplyID != "9" || m.jiraCommentMention == nil {
		t.Errorf("R: active %v key %q reply %q", m.jiraCommentActive, m.jiraCommentKey, m.jiraCommentReplyID)
	}
}

// TestInboxOpen: enter opens the thread's issue in the panel.
func TestInboxOpen(t *testing.T) {
	m := inboxModel(t, &fakeInbox{issues: []string{"ABC-2 Second"}})
	out, _ := m.handleInboxKey(keyMsg(t, "enter"))
	if m = out.(Model); !m.refOpen || m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Error("enter should open the issue")
	}
}

// TestInboxBadge: the header counts unread threads; a failed sync keeps it.
func TestInboxBadge(t *testing.T) {
	m := jiraTabModel(t)
	m.inboxData().threads = []inboxThread{{InboxIssue: jira.InboxIssue{Key: "ABC-1"}, entries: []jira.InboxEntry{{When: time.Now()}}}}
	m.inboxData().seq = 1
	out, _ := m.Update(inboxSyncMsg{seq: 1, threads: m.inboxData().threads})
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "✉ 1 I") {
		t.Fatal("badge not in the header")
	}
	m.inboxData().seq = 2
	out, _ = m.Update(inboxSyncMsg{seq: 2, err: fmt.Errorf("down")})
	if m = out.(Model); m.inboxBadge() == "" {
		t.Error("a failed sync cleared the badge")
	}
}

// TestMentionNotify: a mention after the app started notifies once; older
// ones and repeats stay quiet.
func TestMentionNotify(t *testing.T) {
	m := jiraTabModel(t)
	m.started = time.Now().Add(-time.Hour)
	old := jira.InboxEntry{Who: "Ann", Mention: true, When: m.started.Add(-time.Minute)}
	fresh := jira.InboxEntry{Who: "Bob", Mention: true, When: time.Now()}
	comment := jira.InboxEntry{When: time.Now()}
	m.inboxData().threads = []inboxThread{{InboxIssue: jira.InboxIssue{Key: "ABC-1"}, entries: []jira.InboxEntry{old, fresh, comment}}}
	if m.notifyMentions() == nil {
		t.Fatal("a fresh mention should notify")
	}
	if m.notifyMentions() != nil {
		t.Error("the same mention should not notify twice")
	}
}

func TestInboxOptions(t *testing.T) {
	o, warn := optionsFrom(config.UIConfig{InboxEvery: "off", InboxLookback: "72h", InboxIssues: 50})
	if o.inboxEvery != 0 || o.inboxLookback != 72*time.Hour || o.inboxIssues != 50 || len(warn) != 0 {
		t.Fatalf("options %v %v %d %v", o.inboxEvery, o.inboxLookback, o.inboxIssues, warn)
	}
	m := jiraTabModel(t)
	m.opts = o
	if m.inboxTick() != nil {
		t.Error("ticks with inbox_every off")
	}
	if _, warn = optionsFrom(config.UIConfig{InboxIssues: 999}); len(warn) != 1 {
		t.Errorf("cap unchecked: %v", warn)
	}
}

func TestInboxWhen(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	for at, want := range map[time.Time]string{
		now.Add(-time.Hour):    "11:00",
		now.AddDate(0, 0, -2):  "Mon 12:00",
		now.AddDate(0, 0, 1):   "Thu 12:00",
		now.AddDate(0, 0, -10): "20 Sep",
	} {
		if got := inboxWhen(at, now); got != want {
			t.Errorf("inboxWhen(%v) = %q, want %q", at, got, want)
		}
	}
}
