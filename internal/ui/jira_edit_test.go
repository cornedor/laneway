package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// loadedJiraModel returns a model with the panel open and an issue already
// loaded, so the field-edit hotkeys are live.
func loadedJiraModel(t *testing.T) Model {
	t.Helper()
	m := configuredJiraModel(t, "ABC")
	updated, _ := openRefFor(m, "ABC-1")
	got := updated.(Model)
	final, _ := got.handleJiraLoaded(jiraLoadedMsg{
		gen: got.refGen,
		key: "ABC-1",
		issue: &jira.Issue{
			Key: "ABC-1", Summary: "Fix the widget", Status: "To Do",
			Priority: "Medium", PriorityID: "3",
			Assignee: "Ada", AssigneeAccountID: "acc-1",
			StoryPoints: "5",
		},
	})
	return final.(Model)
}

func TestJiraHotkeyOpensStatusPicker(t *testing.T) {
	m := loadedJiraModel(t)
	updated, cmd := m.handleRefKey(keyStr("s"))
	got := updated.(Model)
	if !got.jiraPicker.active || got.jiraPicker.kind != jiraPickStatus {
		t.Fatalf("status picker not active: %+v", got.jiraPicker)
	}
	if !got.jiraPicker.loading {
		t.Error("expected loading state until the fetch returns")
	}
	if got.jiraPicker.issueKey != "ABC-1" {
		t.Errorf("issueKey = %q", got.jiraPicker.issueKey)
	}
	if cmd == nil {
		t.Error("expected a fetch Cmd")
	}
}

func TestJiraHotkeyNoIssueIsNoop(t *testing.T) {
	// While the issue is still loading (no jiraIssue), the edit keys must not
	// open a picker — they fall through to the viewport.
	m := configuredJiraModel(t, "ABC")
	updated, _ := openRefFor(m, "ABC-1")
	got := updated.(Model) // jiraIssue is nil (fetch not run)
	updated, _ = got.handleRefKey(keyStr("p"))
	if updated.(Model).jiraPicker.active {
		t.Error("picker should not open before an issue is loaded")
	}
}

func TestJiraHotkeyOpensPointsInput(t *testing.T) {
	m := loadedJiraModel(t)
	updated, _ := m.handleRefKey(keyStr("P"))
	got := updated.(Model)
	if !got.jiraFieldActive {
		t.Fatal("points input not active")
	}
	if got.jiraFieldInput.Value() != "5" {
		t.Errorf("points input seeded %q, want 5", got.jiraFieldInput.Value())
	}
	if got.jiraFieldKey != "ABC-1" {
		t.Errorf("points key = %q", got.jiraFieldKey)
	}
}

func TestJiraPickerLoadedSelectsCurrent(t *testing.T) {
	m := loadedJiraModel(t)
	m.startJiraPicker(jiraPickPriority, "Set priority", false)
	updated, _ := m.handleJiraPickerLoaded(jiraPickerLoadedMsg{
		gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickPriority,
		items: []jiraPickerItem{
			{id: "1", label: "Highest"},
			{id: "3", label: "Medium", current: true},
			{id: "5", label: "Low"},
		},
	})
	got := updated.(Model)
	if got.jiraPicker.loading {
		t.Error("still loading after result")
	}
	if got.jiraPicker.idx != 1 {
		t.Errorf("cursor idx = %d, want 1 (the current value)", got.jiraPicker.idx)
	}
}

func TestJiraPickerLoadedDropsStale(t *testing.T) {
	m := loadedJiraModel(t)
	m.startJiraPicker(jiraPickPriority, "Set priority", false)
	updated, _ := m.handleJiraPickerLoaded(jiraPickerLoadedMsg{
		gen: m.jiraPicker.gen + 99, kind: jiraPickPriority,
		items: []jiraPickerItem{{id: "1", label: "Highest"}},
	})
	if !updated.(Model).jiraPicker.loading {
		t.Error("a stale (wrong-gen) result should be ignored, leaving loading set")
	}
}

// pickerHasLabel reports whether the picker currently lists an item labelled
// (prefix-matched) with want.
func pickerHasLabel(m Model, want string) bool {
	for _, it := range m.jiraPicker.items {
		if strings.HasPrefix(it.label, want) {
			return true
		}
	}
	return false
}

func TestJiraAssigneeServerSearch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/user/assignable/search":
			gotQuery = r.URL.Query().Get("query")
			if gotQuery == "alan" {
				_, _ = w.Write([]byte(`[{"accountId":"a2","displayName":"Alan Turing"}]`))
			} else {
				_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Ada Lovelace"},{"accountId":"a2","displayName":"Alan Turing"}]`))
			}
		case "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"me-1","displayName":"Me"}`))
		}
	}))
	defer srv.Close()

	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})

	// Open the picker; the initial empty-query search runs and shows the meta
	// rows plus the default page.
	cmd := m.openJiraAssigneePicker()
	if cmd == nil {
		t.Fatal("expected an initial search Cmd")
	}
	updated, _ := m.handleJiraPickerLoaded(cmd().(jiraPickerLoadedMsg))
	m = updated.(Model)
	if gotQuery != "" {
		t.Errorf("initial query = %q, want empty", gotQuery)
	}
	if !pickerHasLabel(m, "Unassigned") || !pickerHasLabel(m, "Ada Lovelace") {
		t.Errorf("default items = %+v", m.jiraPicker.items)
	}

	// Type "alan"; each change bumps fetchSeq so stale responses are dropped.
	for _, r := range "alan" {
		u, _ := m.handleJiraPickerKey(keyStr(string(r)))
		m = u.(Model)
	}
	if m.jiraPicker.filter.Value() != "alan" {
		t.Fatalf("filter value = %q, want alan", m.jiraPicker.filter.Value())
	}
	if m.jiraPicker.fetchSeq <= 1 {
		t.Errorf("fetchSeq = %d, expected typing to advance it", m.jiraPicker.fetchSeq)
	}

	// A debounce for a superseded query is ignored.
	if _, c := m.handleJiraAssigneeDebounce(jiraAssigneeDebounceMsg{seq: 1}); c != nil {
		t.Error("stale debounce should be ignored")
	}

	// The latest debounce runs the server search; the results replace the list.
	updated, fetchCmd := m.handleJiraAssigneeDebounce(jiraAssigneeDebounceMsg{seq: m.jiraPicker.fetchSeq})
	m = updated.(Model)
	if fetchCmd == nil {
		t.Fatal("debounce should trigger the server fetch")
	}
	updated, _ = m.handleJiraPickerLoaded(fetchCmd().(jiraPickerLoadedMsg))
	m = updated.(Model)

	if gotQuery != "alan" {
		t.Errorf("server query = %q, want alan", gotQuery)
	}
	if pickerHasLabel(m, "Unassigned") {
		t.Error("meta rows should be hidden while searching")
	}
	if !pickerHasLabel(m, "Alan Turing") || pickerHasLabel(m, "Ada Lovelace") {
		t.Errorf("search items = %+v", m.jiraPicker.items)
	}
}

func TestJiraPickerMoveClamps(t *testing.T) {
	m := loadedJiraModel(t)
	m.startJiraPicker(jiraPickPriority, "Set priority", false)
	m.jiraPicker.loading = false
	m.jiraPicker.items = []jiraPickerItem{{id: "1"}, {id: "2"}}
	m.jiraPicker.idx = 0
	m.jiraPickerMove(-1) // already at top
	if m.jiraPicker.idx != 0 {
		t.Errorf("idx = %d, want 0", m.jiraPicker.idx)
	}
	m.jiraPickerMove(5) // past the end
	if m.jiraPicker.idx != 1 {
		t.Errorf("idx = %d, want 1 (clamped)", m.jiraPicker.idx)
	}
}

func TestApplyJiraPickSetsPriority(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.startJiraPicker(jiraPickPriority, "Set priority", false)
	m.jiraPicker.loading = false
	m.jiraPicker.items = []jiraPickerItem{{id: "1", label: "Highest"}, {id: "3", label: "Medium"}}
	m.jiraPicker.idx = 0

	updated, cmd := m.applyJiraPick()
	got := updated.(Model)
	if got.jiraPicker.active {
		t.Error("picker should close on apply")
	}
	if !strings.Contains(got.status, "updating ABC-1 priority") {
		t.Errorf("status = %q", got.status)
	}
	if cmd == nil {
		t.Fatal("expected a mutation Cmd")
	}
	msg, ok := cmd().(jiraMutatedMsg)
	if !ok {
		t.Fatalf("cmd returned %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("mutation failed: %v", msg.err)
	}
	if gotMethod != http.MethodPut || gotPath != "/rest/api/3/issue/ABC-1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"1"`) || !strings.Contains(gotBody, "priority") {
		t.Errorf("body = %q", gotBody)
	}
	if msg.field != "priority" || msg.key != "ABC-1" {
		t.Errorf("msg = %+v", msg)
	}
}

func TestApplyJiraPointsClears(t *testing.T) {
	const fieldMeta = `[{"id":"customfield_10016","name":"Story point estimate"}]`
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			_, _ = w.Write([]byte(fieldMeta))
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	m.openJiraPointsInput()
	m.jiraFieldInput.SetValue("") // clear the seeded value

	updated, cmd := m.applyJiraField()
	if updated.(Model).jiraFieldActive {
		t.Error("points input should close on apply")
	}
	if cmd == nil {
		t.Fatal("expected a mutation Cmd")
	}
	if msg := cmd().(jiraMutatedMsg); msg.err != nil {
		t.Fatalf("mutation failed: %v", msg.err)
	}
	if !strings.Contains(gotBody, `"customfield_10016":null`) {
		t.Errorf("body = %q, want field cleared to null", gotBody)
	}
}

func TestHandleJiraMutatedReloadsOnSuccess(t *testing.T) {
	m := loadedJiraModel(t)
	prevGen := m.refGen
	updated, cmd := m.handleJiraMutated(jiraMutatedMsg{key: "ABC-1", field: "status"})
	got := updated.(Model)
	if cmd == nil || got.refGen == prevGen {
		t.Error("success should reload the issue (bump refGen, return a fetch Cmd)")
	}
	if !strings.Contains(got.status, "updated") {
		t.Errorf("status = %q", got.status)
	}
}

func TestHandleJiraMutatedError(t *testing.T) {
	m := loadedJiraModel(t)
	updated, cmd := m.handleJiraMutated(jiraMutatedMsg{key: "ABC-1", field: "status", err: fmt.Errorf("boom")})
	got := updated.(Model)
	if cmd != nil {
		t.Error("error path should not reload")
	}
	if !strings.Contains(got.status, "failed") {
		t.Errorf("status = %q", got.status)
	}
}

func TestJiraPickerWindowsLongList(t *testing.T) {
	const maxH = 20 // small body area so the window is well under the list size
	m := loadedJiraModel(t)
	m.startJiraPicker(jiraPickAssignee, "Set assignee", true)
	m.jiraPicker.loading = false
	items := make([]jiraPickerItem, 0, 50)
	for i := 0; i < 50; i++ {
		items = append(items, jiraPickerItem{id: fmt.Sprintf("a%d", i), label: fmt.Sprintf("User %02d", i)})
	}
	m.jiraPicker.items = items

	// Walk the cursor to the bottom; the render windows around it.
	for i := 0; i < len(items)-1; i++ {
		m.jiraPickerMove(1)
	}
	if m.jiraPicker.idx != len(items)-1 {
		t.Fatalf("cursor idx = %d, want %d", m.jiraPicker.idx, len(items)-1)
	}

	// The popup must fit within maxH, window the list (not render all 50), show
	// a "more above" indicator, and keep the selected row visible.
	out := m.renderJiraPicker(maxH)
	if lines := strings.Count(out, "\n") + 1; lines > maxH {
		t.Errorf("rendered picker is %d lines, exceeds maxH %d", lines, maxH)
	}
	if !strings.Contains(out, "more") {
		t.Errorf("expected a scroll indicator, got:\n%s", out)
	}
	if strings.Contains(out, "User 00") {
		t.Error("first item visible while scrolled to the bottom — list did not window")
	}
	if !strings.Contains(out, "User 49") {
		t.Error("selected (last) item not visible in the window")
	}
}

func TestJiraPickerKeyEscCloses(t *testing.T) {
	m := loadedJiraModel(t)
	m.startJiraPicker(jiraPickStatus, "Set status", false)
	updated, _ := m.handleJiraPickerKey(keyStr("esc"))
	if updated.(Model).jiraPicker.active {
		t.Error("esc should close the picker")
	}
}

// TestJiraEditSummary: e opens the input seeded with the summary; enter
// writes the trimmed text, an unchanged one closes without a write and an
// empty one is refused.
func TestJiraEditSummary(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/ABC-1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, _ := m.handleRefKey(keyStr("e"))
	m = out.(Model)
	if !m.jiraFieldActive || m.jiraFieldName != "summary" || m.jiraFieldInput.Value() != "Fix the widget" {
		t.Fatalf("input: active %v, field %q, value %q", m.jiraFieldActive, m.jiraFieldName, m.jiraFieldInput.Value())
	}
	if v := ansi.Strip(m.View().Content); strings.Contains(v, "Edit summary") || !strings.Contains(v, "❯ Fix the widget") {
		t.Error("summary should edit inline in its row, not in a modal")
	}
	out, _ = m.handleKey(keyStr("!"))
	if m = out.(Model); !strings.Contains(ansi.Strip(m.View().Content), "❯ Fix the widget!") {
		t.Error("typing should redraw the row")
	}
	m.jiraFieldInput.SetValue("Fix the widget")
	if out, cmd := m.applyJiraField(); cmd != nil || out.(Model).jiraFieldActive {
		t.Error("unchanged summary should close without a write")
	}
	m.jiraFieldInput.SetValue("  ")
	out, cmd := m.applyJiraField()
	if cmd != nil || !out.(Model).jiraFieldActive || !strings.Contains(out.(Model).status, "empty") {
		t.Error("empty summary should be refused and keep the input")
	}
	m.jiraFieldInput.SetValue(" Fix the gadget ")
	out, cmd = m.applyJiraField()
	if cmd == nil || out.(Model).jiraFieldActive {
		t.Fatal("expected a write and a closed input")
	}
	if msg := cmd().(jiraMutatedMsg); msg.err != nil || msg.field != "summary" {
		t.Fatalf("mutation: %+v", msg)
	}
	if gotBody != `{"fields":{"summary":"Fix the gadget"}}` {
		t.Errorf("body = %q", gotBody)
	}
}

// TestJiraEditLabels: l opens the labels space separated; enter writes the
// set, unchanged closes without a write, and empty clears.
func TestJiraEditLabels(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := loadedJiraModel(t)
	m.jiraIssue.Labels = []string{"backend", "urgent"}
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, _ := m.handleRefKey(keyStr("l"))
	m = out.(Model)
	if m.jiraFieldName != "labels" || m.jiraFieldInput.Value() != "backend urgent" {
		t.Fatalf("input: field %q, value %q", m.jiraFieldName, m.jiraFieldInput.Value())
	}
	m.jiraFieldInput.SetValue(" backend   urgent ")
	if _, cmd := m.applyJiraField(); cmd != nil {
		t.Error("unchanged labels should not write")
	}
	for _, v := range []string{"backend ui", ""} {
		m.openJiraLabelsInput()
		m.jiraFieldInput.SetValue(v)
		_, cmd := m.applyJiraField()
		if cmd == nil {
			t.Fatalf("%q: no write", v)
		}
		if msg := cmd().(jiraMutatedMsg); msg.err != nil {
			t.Fatal(msg.err)
		}
	}
	if len(bodies) != 2 || bodies[0] != `{"fields":{"labels":["backend","ui"]}}` || bodies[1] != `{"fields":{"labels":[]}}` {
		t.Errorf("bodies = %q", bodies)
	}
}

// TestCommentThread: replies sit under their parent, a reply to a comment
// not loaded stands alone, and a parentId loop can't hide comments.
func TestCommentThread(t *testing.T) {
	cs := []jira.Comment{
		{ID: "1"}, {ID: "2"}, {ID: "3", ParentID: "1"}, {ID: "4", ParentID: "3"},
		{ID: "5", ParentID: "99"},                          // parent not loaded
		{ID: "6", ParentID: "7"}, {ID: "7", ParentID: "6"}, // a loop
		{ID: "8", ParentID: "8"}, // its own parent
	}
	var got []string
	for _, tc := range commentThread(cs) {
		got = append(got, fmt.Sprintf("%s:%d", cs[tc.i].ID, tc.depth))
	}
	if want := "1:0 3:1 4:2 2:0 5:0 8:0 6:0 7:1"; strings.Join(got, " ") != want {
		t.Errorf("thread = %s, want %s", strings.Join(got, " "), want)
	}
}

// TestCommentRepliesIndented: a reply renders behind a bar under its parent.
func TestCommentRepliesIndented(t *testing.T) {
	m := loadedJiraModel(t)
	iss := &jira.Issue{Key: "ABC-1", Comments: []jira.Comment{
		{ID: "1", Author: "Ada", Body: "Question?"},
		{ID: "2", Author: "Bob", Body: "Unrelated"},
		{ID: "3", ParentID: "1", Author: "Cy", Body: "Answer."},
	}}
	got := ansi.Strip(m.renderJiraIssue(iss, 60))
	q, a, u := strings.Index(got, "Question?"), strings.Index(got, "│ Cy"), strings.Index(got, "Unrelated")
	if q < 0 || a < q || u < a || !strings.Contains(got, "│   Answer.") {
		t.Errorf("thread not drawn:\n%s", got)
	}
}

// TestInlineStatusPicker: s drops the transitions under the Status row, not
// in a modal; a click on a row applies it, and the list goes.
func TestInlineStatusPicker(t *testing.T) {
	m := loadedJiraModel(t)
	out, _ := m.Update(keyStr("s"))
	m = out.(Model)
	out, _ = m.Update(jiraPickerLoadedMsg{gen: m.jiraPicker.gen, seq: m.jiraPicker.fetchSeq, kind: jiraPickStatus,
		items: []jiraPickerItem{{id: "11", label: "To Do", current: true}, {id: "21", label: "In Review"}}})
	m = out.(Model)
	v := ansi.Strip(m.View().Content)
	if strings.Contains(v, "Set status") {
		t.Fatal("status picker drawn as a modal")
	}
	if !regexp.MustCompile(`Status: .*\n.*│ +▸ ✓ To Do .*\n.*│ +In Review`).MatchString(v) {
		t.Fatalf("list not under the Status row:\n%s", v)
	}
	y := -1
	for i, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "In Review") {
			y = i
		}
	}
	listW, _ := m.jiraListWidth(m.width)
	out, cmd := m.Update(tea.MouseClickMsg{X: listW + 12, Y: y, Button: tea.MouseLeft})
	m = out.(Model)
	if m.jiraPicker.active || cmd == nil {
		t.Fatalf("click should apply In Review: active %v", m.jiraPicker.active)
	}
	if strings.Contains(ansi.Strip(m.View().Content), "▸ ✓ To Do") {
		t.Error("list still drawn after the pick")
	}
}
