package ui

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
)

func tempIndex(t *testing.T, cards ...jira.Card) *index.Index {
	t.Helper()
	ix, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	ix.PutCards(cards)
	return ix
}

// batchMsgs runs a batch's commands, in order.
func batchMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	b, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("want a batch")
	}
	var out []tea.Msg
	for _, c := range b {
		out = append(out, c())
	}
	return out
}

// TestPaletteIndexFirst: the index's hits show before Jira answers, marked
// with when they were read; Jira's come first once it does, and offline the
// index's stay.
func TestPaletteIndexFirst(t *testing.T) {
	down := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"issues":[{"key":"OPS-7","fields":{"summary":"Third party outage"}}]}`)
	}))
	defer srv.Close()
	for _, offline := range []bool{false, true} {
		down = offline
		m := jiraTabModel(t)
		url := srv.URL
		if down {
			l, _ := net.Listen("tcp", "127.0.0.1:0")
			url = "http://" + l.Addr().String()
			l.Close() // nothing listens: the dial fails
		}
		m.jiraClient = jira.New(jira.Config{BaseURL: url, Email: "me@x.test", APIToken: "tok"})
		m = m.WithIndex(tempIndex(t, jira.Card{Key: "XY-9", Summary: "Third wheel"}))
		m = typePalette(t, m, "thi")
		_, cmd := m.handlePaletteSearch(paletteSearchMsg{m.jiraPicker.fetchSeq})
		msgs := batchMsgs(t, cmd)
		out, _ := m.handlePaletteFound(msgs[0].(paletteFoundMsg))
		m = out.(Model)
		if got := paletteLabels(m); len(got) != 2 || !strings.HasPrefix(got[1], "⌕ XY-9  Third wheel  · ") {
			t.Fatalf("before Jira: rows = %q", got)
		}
		out, _ = m.handlePaletteFound(msgs[1].(paletteFoundMsg))
		m = out.(Model)
		got := paletteLabels(m)
		switch {
		case !offline && (len(got) != 3 || got[1] != "⌕ OPS-7  Third party outage" || !strings.HasPrefix(got[2], "⌕ XY-9")):
			t.Errorf("after Jira: rows = %q", got)
		case offline && (len(got) != 2 || !strings.HasPrefix(got[1], "⌕ XY-9") || !strings.Contains(m.status, "offline")):
			t.Errorf("offline: rows = %q, status %q", got, m.status)
		}
	}
}

// TestPanelOfflineFromIndex: a panel read that can't reach Jira shows the
// indexed card and says so; another error, or a key not indexed, doesn't.
func TestPanelOfflineFromIndex(t *testing.T) {
	m := configuredJiraModel(t, "ABC")
	m = m.WithIndex(tempIndex(t, jira.Card{Key: "ABC-1", Summary: "Fix the widget", Status: "Doing", InProgress: true}))
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	for _, tc := range []struct {
		key  string
		err  error
		want bool
	}{
		{"ABC-1", dial, true},
		{"ABC-1", errors.New("404: issue does not exist"), false},
		{"ABC-2", dial, false},
	} {
		out, _ := openRefFor(m, tc.key)
		mm := out.(Model)
		out, _ = mm.handleJiraLoaded(jiraLoadedMsg{gen: mm.refGen, key: tc.key, err: tc.err})
		mm = out.(Model)
		got := mm.jiraIssue != nil && mm.jiraIssue.Summary == "Fix the widget" && mm.jiraIssue.StatusCategory == "indeterminate"
		if got != tc.want || got && !strings.Contains(mm.status, "from the index") {
			t.Errorf("%s, %v: issue %+v, status %q", tc.key, tc.err, mm.jiraIssue, mm.status)
		}
	}
}

// TestJQLViewOffline: offline, a Q search answers from the index what it
// can and says what it left out.
func TestJQLViewOffline(t *testing.T) {
	m := jiraTabModel(t)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	url := "http://" + l.Addr().String()
	l.Close() // nothing listens: the dial fails
	m.jiraClient = jira.New(jira.Config{BaseURL: url, Email: "me@x.test", APIToken: "tok"})
	m.index = tempIndex(t, jira.Card{Key: "OPS-1", Summary: "Outage", Status: "To Do"}, jira.Card{Key: "OPS-2", Summary: "Fixed", Status: "Done", Done: true},
		jira.Card{Key: "ABC-9", Summary: "Other", Status: "To Do"})
	cmd := m.runJQLView("project = OPS AND statusCategory != Done AND labels = ui")
	msg, ok := cmd().(jiraCardsMsg) // no cache to show first: one command
	if !ok {
		t.Fatal("want the cards")
	}
	out, _ := m.handleJiraCards(msg)
	m = out.(Model)
	if len(m.jiraTab.cards) != 1 || m.jiraTab.cards[0].Key != "OPS-1" {
		t.Errorf("cards = %+v", m.jiraTab.cards)
	}
	if !strings.Contains(m.status, "from the index") || !strings.Contains(m.status, "left out labels = ui") {
		t.Errorf("status = %q", m.status)
	}
}
