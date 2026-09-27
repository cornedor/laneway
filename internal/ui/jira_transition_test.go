package ui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

func codeReviewForm(t *testing.T) Model {
	t.Helper()
	m := jiraTabModel(t)
	tm := jira.TransitionMeta{ID: "15", ToName: "Code review", HasScreen: true, Fields: []jira.FieldMeta{
		{ID: "customfield_1", Name: "Story Points", Kind: jira.KindNumber},
		{ID: "customfield_2", Name: "Code Reviewer", Kind: jira.KindUser},
	}}
	rule := jira.TransitionRule{Required: []string{"customfield_2", "comment"}, Message: "Vul de code reviewer in."}
	ic := jira.IssueContext{Values: map[string]json.RawMessage{"customfield_1": json.RawMessage(`3`)}}
	f := buildJiraForm("ABC-1", tm, rule, ic)
	out, _ := m.handleJiraPrepared(jiraPreparedMsg{key: "ABC-1", to: "Code review", form: f})
	return out.(Model)
}

// TestJiraFormLayout: required fields first, the screen-less comment added,
// the cursor on the first empty required one, current values filled in.
func TestJiraFormLayout(t *testing.T) {
	m := codeReviewForm(t)
	f := m.jiraForm
	var names []string
	for _, ff := range f.fields {
		names = append(names, ff.Name)
	}
	if got := strings.Join(names, ","); got != "Code Reviewer,Comment,Story Points" {
		t.Fatalf("fields = %s", got)
	}
	if f.idx != 0 || f.fields[2].val.Text != "3" {
		t.Errorf("idx=%d points=%q", f.idx, f.fields[2].val.Text)
	}
	view := m.View().Content
	for _, want := range []string{"ABC-1 → Code review", "Vul de code reviewer in.", "required", "Move to Code review"} {
		if !strings.Contains(view, want) {
			t.Errorf("form lacks %q", want)
		}
	}
}

// TestJiraFormSubmit: a move with a required field empty is held back; once
// filled it goes, carrying only what changed plus the comment.
func TestJiraFormSubmit(t *testing.T) {
	m := codeReviewForm(t)
	if cmd := m.submitJiraForm(); cmd != nil || !strings.Contains(m.jiraForm.err, "Code Reviewer, Comment") {
		t.Fatalf("err = %q, want the missing fields named", m.jiraForm.err)
	}
	m.pickJiraFormValue(jiraPickFormUser, jiraPickerItem{id: "a1", label: "Ada"})
	m.jiraForm.idx = 1
	out, _ := m.handleJiraFormKey(keyMsg(t, "enter"))
	m = out.(Model)
	if !m.jiraForm.multiline {
		t.Fatal("enter on the comment did not start editing")
	}
	m.jiraForm.area.SetValue("please review")
	out, _ = m.handleJiraFormKey(keyStr("ctrl+s"))
	m = out.(Model)
	if m.jiraForm.fields[1].val.Text != "please review" {
		t.Fatalf("comment = %q", m.jiraForm.fields[1].val.Text)
	}
	if cmd := m.submitJiraForm(); cmd == nil || !m.jiraForm.busy {
		t.Fatalf("submit held back: %q", m.jiraForm.err)
	}
}

// TestJiraFormErrorKeepsForm: Jira refusing the move leaves the form up with
// its message; esc then puts the board back.
func TestJiraFormErrorKeepsForm(t *testing.T) {
	m := codeReviewForm(t)
	out, _ := m.handleJiraFormDone(jiraFormDoneMsg{key: "ABC-1", err: errors.New("nope")})
	m = out.(Model)
	if m.jiraForm == nil || m.jiraForm.err != "nope" {
		t.Fatalf("form = %+v, want it kept with the error", m.jiraForm)
	}
	out, cmd := m.handleJiraFormKey(keyMsg(t, "esc"))
	m = out.(Model)
	if m.jiraForm != nil || cmd == nil {
		t.Error("esc did not close the form and refetch the board")
	}
}

// TestJiraFormClick: a click selects a row, a second edits it; a click
// elsewhere keeps what was typed; the button moves.
func TestJiraFormClick(t *testing.T) {
	m := codeReviewForm(t)
	at := func(text string) (int, int) {
		for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if x := strings.Index(l, text); x >= 0 {
				return ansi.StringWidth(l[:x]), y
			}
		}
		t.Fatalf("%q not on screen", text)
		return 0, 0
	}
	click := func(text string) tea.Cmd {
		x, y := at(text)
		out, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = out.(Model)
		m.lastClick.at = time.Time{} // no double-clicks between steps
		return cmd
	}
	click("Comment *")
	if f := m.jiraForm; f.idx != 1 || f.editing {
		t.Fatalf("idx %d editing %v", f.idx, f.editing)
	}
	click("Comment *")
	if !m.jiraForm.editing {
		t.Fatal("a second click should edit")
	}
	m.jiraForm.area.SetValue("please review")
	click("Story Points")
	if f := m.jiraForm; f.idx != 2 || f.editing || f.fields[1].val.Text != "please review" {
		t.Fatalf("idx %d editing %v comment %q", f.idx, f.editing, f.fields[1].val.Text)
	}
	click("Move to Code review")
	if f := m.jiraForm; f.idx != len(f.fields) || !strings.Contains(f.err, "Code Reviewer") {
		t.Errorf("button: idx %d err %q", f.idx, f.err)
	}
}

// TestJiraFormMultiline: the comment edits in an editor under its row, enter
// breaking the line and ctrl+s keeping it; a paste lands in it; esc undoes.
func TestJiraFormMultiline(t *testing.T) {
	m := codeReviewForm(t)
	m.jiraForm.idx = 1
	out, _ := m.handleKey(keyStr("enter"))
	for _, k := range []string{"a", "enter", "b"} {
		out, _ = out.(Model).handleKey(keyStr(k))
	}
	out, _ = out.(Model).Update(tea.PasteMsg{Content: "c"})
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "ctrl+s keep") {
		t.Errorf("no editor keys:\n%s", ansi.Strip(m.View().Content))
	}
	out, _ = m.handleKey(keyStr("ctrl+s"))
	m = out.(Model)
	if f := m.jiraForm; f.editing || f.fields[1].val.Text != "a\nbc" {
		t.Fatalf("editing %v, comment %q", f.editing, f.fields[1].val.Text)
	}
	out, _ = m.handleKey(keyStr("enter"))
	out, _ = out.(Model).handleKey(keyStr("x"))
	out, _ = out.(Model).handleKey(keyStr("esc"))
	if m = out.(Model); m.jiraForm == nil || m.jiraForm.fields[1].val.Text != "a\nbc" {
		t.Errorf("esc should undo only the edit: %+v", m.jiraForm)
	}
}
