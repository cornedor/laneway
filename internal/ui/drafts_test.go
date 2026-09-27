package ui

import (
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestCommentDraft: a comment typed is kept a moment later; a new session
// on the same state brings it back, and posting it drops it.
func TestCommentDraft(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraIssue = &jira.Issue{Key: "ABC-1"}
	m.openJiraCommentInput()
	for _, k := range []string{"h", "i"} {
		out, cmd := m.handleJiraCommentKey(keyStr(k))
		m = out.(Model)
		if k == "h" && cmd == nil {
			t.Fatal("typing should schedule a draft save")
		}
	}
	out, _ := m.Update(draftSaveMsg{})
	m = out.(Model)
	if text, _, ok := m.draft(commentDraft("ABC-1")); !ok || text != "hi" {
		t.Fatalf("draft %q %v", text, ok)
	}

	next := jiraTabModel(t)
	next.store = m.store // the app started again
	next.jiraIssue = &jira.Issue{Key: "ABC-1"}
	next.openJiraCommentInput()
	if next.jiraCommentInput.Value() != "hi" || !strings.Contains(next.status, "your draft from") {
		t.Fatalf("reopened with %q, status %q", next.jiraCommentInput.Value(), next.status)
	}
	out, _ = next.applyJiraComment()
	next = out.(Model)
	out, _ = next.handleJiraMutated(jiraMutatedMsg{key: "ABC-1", field: "comment", text: "hi"})
	next = out.(Model)
	if _, _, ok := next.draft(commentDraft("ABC-1")); ok {
		t.Error("a posted comment should drop its draft")
	}
}

// TestDescDraft: the description editor keeps its text on quit and offers
// it over Jira's on reopen; esc twice drops it.
func TestDescDraft(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraIssue = &jira.Issue{Key: "ABC-1"}
	m.refOpen = true
	m.refs = []reference{{kind: refJira, jiraKey: "ABC-1"}}
	out, _ := m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "old"})
	m = out.(Model)
	m.descEdit.input.SetValue("new text")
	out, _ = m.quit()
	m = out.(Model) // asks first; the draft is kept either way
	m.quitAsked = true
	m.quit()
	if text, _, ok := m.draft("desc:ABC-1"); !ok || text != "new text" {
		t.Fatalf("draft %q %v", text, ok)
	}
	m.descEdit = nil
	out, _ = m.handleDescLoaded(descLoadedMsg{key: "ABC-1", md: "old"})
	m = out.(Model)
	if m.descEdit.input.Value() != "new text" || m.descEdit.before != "old" {
		t.Fatalf("reopened with %q over %q", m.descEdit.input.Value(), m.descEdit.before)
	}
	for range 2 {
		out, _ = m.handleDescEditKey(keyMsg(t, "esc"))
		m = out.(Model)
	}
	if _, _, ok := m.draft("desc:ABC-1"); ok || m.descEdit != nil {
		t.Error("esc twice should drop the draft")
	}
}
