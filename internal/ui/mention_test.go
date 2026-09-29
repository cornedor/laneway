package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	if !strings.Contains(ansi.Strip(m.View().Content), "🚀  :rocket:") {
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
	for _, text := range []string{"at 10:30", "mail me:ab", "ok :)", "hm :-)", ":a", "done :tada: ok"} {
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

// TestEmojiMatches: exact, then prefix, then inside, then the letters in
// order; one taken before rises within its band.
func TestEmojiMatches(t *testing.T) {
	m := loadedJiraModel(t)
	if got := m.emojiMatches("smile"); got[0] != "smile" {
		t.Errorf("exact first: %v", got)
	}
	if got := m.emojiMatches("smle"); !slices.Contains(got, "smile") {
		t.Errorf("fuzzy: %v", got)
	}
	got := m.emojiMatches("rock")
	if got[0] != "rock" || got[1] != "rocket" {
		t.Fatalf("rock: %v", got)
	}
	m.openJiraCommentInput()
	m.jiraCommentInput.SetValue(":rocke")
	m.jiraCommentInput.CursorEnd()
	m.scheduleMention()
	m.acceptEmoji("rocket")
	if got := m.emojiMatches("roc"); got[0] != "rocket" {
		t.Errorf("taken before should lead its band: %v", got)
	}
}

// TestEmojiListKeys: while the list shows, ↓ chooses, enter takes (not a
// newline) and esc closes it, the comment kept; in the description editor
// too.
func TestEmojiListKeys(t *testing.T) {
	m := loadedJiraModel(t)
	m.openJiraCommentInput()
	type_ := func(s string) {
		t.Helper()
		for _, r := range s {
			out, _ := m.handleJiraCommentKey(keyMsg(t, string(r)))
			m = out.(Model)
		}
	}
	type_("hi :tad")
	if len(m.jiraMention.emoji) == 0 || m.jiraMention.emoji[0] != "tada" {
		t.Fatalf("offered %v", m.jiraMention.emoji)
	}
	out, _ := m.handleJiraCommentKey(keyMsg(t, "enter"))
	if m = out.(Model); m.jiraCommentInput.Value() != "hi :tada: " {
		t.Fatalf("enter: %q", m.jiraCommentInput.Value())
	}
	type_(":smi")
	out, _ = m.handleJiraCommentKey(keyMsg(t, "esc"))
	if m = out.(Model); !m.jiraCommentActive || len(m.jiraMention.emoji) != 0 || m.jiraCommentDiscard {
		t.Fatalf("esc should close the list only: active %v list %v", m.jiraCommentActive, m.jiraMention.emoji)
	}

	m = loadedJiraModel(t)
	out, _ = m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "see :rocke"})
	m = out.(Model)
	m.descEdit.input.CursorEnd()
	out, _ = m.handleDescEditKey(keyMsg(t, "t"))
	m = out.(Model)
	if len(m.jiraMention.emoji) == 0 || !strings.Contains(ansi.Strip(m.renderDescEdit()), ":rocket:") {
		t.Fatalf("description editor: %v", m.jiraMention.emoji)
	}
	out, _ = m.handleDescEditKey(keyMsg(t, "tab"))
	if m = out.(Model); m.descEdit.input.Value() != "see :rocket: " {
		t.Errorf("tab: %q", m.descEdit.input.Value())
	}
}

// TestMentionListInView: under a long thread the composer sits at the
// panel's foot; the list the search brings scrolls into view with it.
func TestMentionListInView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"accountId":"a1","displayName":"Claude"}]`)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	for i := range 30 {
		m.jiraIssue.Comments = append(m.jiraIssue.Comments, jira.Comment{ID: string(rune('a' + i)), Author: "Ann", Body: "a comment"})
	}
	m.renderRef()
	out, _ := m.handleRefKey(keyStr("c"))
	m = out.(Model)
	for _, k := range []string{"@", "c", "l"} {
		out, _ = m.Update(keyMsg(t, k))
		m = out.(Model)
	}
	out, cmd := m.Update(mentionSearchMsg{m.jiraMention.seq, "cl"})
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if !m.commentInline() {
		t.Fatal("composer should sit in the panel")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "▸ @Claude") {
		t.Fatalf("suggestion not in view:\n%s", ansi.Strip(m.View().Content))
	}
}
