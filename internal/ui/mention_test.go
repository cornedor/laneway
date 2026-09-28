package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

func TestMentionAt(t *testing.T) {
	for _, c := range []struct {
		text  string
		q     string
		start int
		ok    bool
	}{
		{"hi @ad", "ad", 3, true},
		{"@Ann", "Ann", 0, true},
		{"mail me@x", "", 0, false},
		{"hi @", "", 0, false},
		{"hi @ada lov", "", 0, false},
	} {
		q, start, ok := mentionAt(c.text, len([]rune(c.text)))
		if q != c.q || start != c.start || ok != c.ok {
			t.Errorf("%q: %q %d %v", c.text, q, start, ok)
		}
	}
}

// TestMentionFlow: "@ad" finds Ada, tab writes her name, posting sends a
// real mention.
func TestMentionFlow(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `[{"accountId":"a1","displayName":"Ada Lovelace"}]`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		posted = string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.openJiraCommentInput()
	m.jiraCommentInput.SetValue("thanks @ad")
	m.jiraCommentInput.CursorEnd()
	cmd := m.scheduleMention()
	out, cmd := m.handleMentionSearch(cmd().(mentionSearchMsg))
	m = out.(Model)
	out, _ = m.Update(cmd().(mentionFoundMsg))
	m = out.(Model)
	if !strings.Contains(m.View().Content, "@Ada Lovelace") {
		t.Fatal("suggestion not shown")
	}
	out, _ = m.handleJiraCommentKey(keyMsg(t, "tab"))
	m = out.(Model)
	if got := m.jiraCommentInput.Value(); got != "thanks @Ada Lovelace " {
		t.Fatalf("value = %q", got)
	}
	_, cmd = m.applyJiraComment()
	cmd()
	if !strings.Contains(posted, `"type":"mention"`) || !strings.Contains(posted, `"id":"a1"`) {
		t.Errorf("posted = %s", posted)
	}
}

// TestEmojiFlow: ":rocke" offers the rocket, tab writes it, posting sends
// Jira's emoji; a comment's emoji shows as its glyph.
func TestEmojiFlow(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		posted = string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.openJiraCommentInput()
	m.jiraCommentInput.SetValue("ship it :rocke")
	m.jiraCommentInput.CursorEnd()
	if cmd := m.scheduleMention(); cmd != nil || len(m.jiraMention.emoji) == 0 || m.jiraMention.emoji[0] != "rocket" {
		t.Fatalf("offered %v", m.jiraMention.emoji)
	}
	m.renderRef() // as Update does after a key
	if !strings.Contains(ansi.Strip(m.View().Content), "🚀 :rocket:") {
		t.Fatal("emoji not shown")
	}
	out, _ := m.handleJiraCommentKey(keyMsg(t, "tab"))
	m = out.(Model)
	if got := m.jiraCommentInput.Value(); got != "ship it :rocket: " {
		t.Fatalf("value = %q", got)
	}
	_, cmd := m.applyJiraComment()
	cmd()
	if !strings.Contains(posted, `"shortName":":rocket:"`) || !strings.Contains(posted, `"type":"emoji"`) {
		t.Errorf("posted = %s", posted)
	}
	for _, text := range []string{"at 10:30", "mail me:ab"} {
		if _, _, ok := emojiQueryAt(text, len(text)); ok {
			t.Errorf("%q offered emoji", text)
		}
	}

	m = loadedJiraModel(t)
	m.jiraIssue.Comments = []jira.Comment{{ID: "1", Author: "Ann", Body: "nice 👍🏽:tada:"}} // as jira reads the emoji nodes (TestEmojiShows)
	m.renderRef()
	if got := ansi.Strip(m.refView.GetContent()); !strings.Contains(got, "nice 👍🏽🎉") {
		t.Errorf("comment shows:\n%s", got)
	}
}
