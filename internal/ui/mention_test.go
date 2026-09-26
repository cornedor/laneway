package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	out, _ = m.handleMentionFound(cmd().(mentionFoundMsg))
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
