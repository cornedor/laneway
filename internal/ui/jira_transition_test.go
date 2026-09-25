package ui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"jiratui/internal/jira"
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
	if !m.jiraForm.editing {
		t.Fatal("enter on the comment did not start editing")
	}
	m.jiraForm.input.SetValue("please review")
	out, _ = m.handleJiraFormKey(keyMsg(t, "enter"))
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
