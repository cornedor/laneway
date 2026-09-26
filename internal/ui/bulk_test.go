package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// bulkModel is the board with ABC-1 and ABC-3 marked (the To do lane), on a
// fake Jira that records writes. ABC-3 offers no move to Done.
func bulkModel(t *testing.T) (Model, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/transitions") {
			if strings.Contains(r.URL.Path, "ABC-3") {
				io.WriteString(w, `{"transitions":[{"id":"11","to":{"name":"In progress"}}]}`)
			} else {
				io.WriteString(w, `{"transitions":[{"id":"11","to":{"name":"In progress"}},{"id":"31","to":{"name":"Done"}}]}`)
			}
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	for range 2 {
		out, _ := m.handleJiraKey(keyMsg(t, "x"))
		m = out.(Model)
	}
	return m, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(writes)
		slices.Sort(out)
		return out
	}
}

// pickBulk opens B and picks the row whose id is id.
func pickBulk(t *testing.T, m Model, id string) Model {
	t.Helper()
	out, _ := m.handleJiraKey(keyMsg(t, "B"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.kind != jiraPickBulk {
		t.Fatal("B should open the bulk menu")
	}
	m.jiraPicker.idx = slices.IndexFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == id })
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	if cmd != nil {
		if msg, ok := cmd().(jiraPickerLoadedMsg); ok {
			out, _ = m.handleJiraPickerLoaded(msg)
			m = out.(Model)
		}
	}
	return m
}

func TestBulkMark(t *testing.T) {
	m, _ := bulkModel(t)
	if got := m.markedKeys(); !slices.Equal(got, []string{"ABC-1", "ABC-3"}) {
		t.Fatalf("marked = %v", got)
	}
	if n := strings.Count(ansi.Strip(m.View().Content), "✓"); n != 2 {
		t.Errorf("%d marks drawn, want 2", n)
	}
	out, _ := m.handleJiraKey(keyMsg(t, "esc"))
	if m = out.(Model); len(m.markedKeys()) != 0 {
		t.Error("esc should clear the marks")
	}
	out, _ = m.handleJiraKey(keyMsg(t, "B"))
	if m = out.(Model); m.jiraPicker.active || !strings.Contains(m.status, "mark cards") {
		t.Error("B without marks should say how to mark")
	}
}

// TestBulkStatus: each issue moves along its own transition to the picked
// status; one without that move fails and stays marked.
func TestBulkStatus(t *testing.T) {
	m, writes := bulkModel(t)
	m = pickBulk(t, m, "status")
	m.jiraPicker.idx = slices.IndexFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.label == "Done" })
	out, cmd := m.applyJiraPick()
	m = out.(Model)
	check := cmd().(bulkMoveMsg) // no screen: straight on
	if check.form != nil || check.err != nil {
		t.Fatalf("check = %+v", check)
	}
	out, cmd = m.handleBulkMove(check)
	m = out.(Model)
	msg := cmd().(bulkDoneMsg)
	if len(msg.failed) != 1 || msg.failed["ABC-3"] == nil {
		t.Fatalf("failed = %v", msg.failed)
	}
	out, _ = m.handleBulkDone(msg)
	m = out.(Model)
	if got := m.markedKeys(); !slices.Equal(got, []string{"ABC-3"}) || !strings.Contains(m.status, "no move to Done") {
		t.Errorf("marked %v, status %q", got, m.status)
	}
	if w := writes(); len(w) != 1 || !strings.HasPrefix(w[0], "POST /rest/api/3/issue/ABC-1/transitions") {
		t.Errorf("writes = %q", w)
	}
}

// TestBulkPriority: the picked priority on every marked card.
func TestBulkPriority(t *testing.T) {
	m, writes := bulkModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "B"))
	m = out.(Model)
	m.jiraPicker.idx = 1 // Priority
	out, _ = m.applyJiraPick()
	m = out.(Model)
	m.setJiraPickerItems([]jiraPickerItem{{id: "2", label: "High"}})
	_, cmd := m.applyJiraPick()
	if msg := cmd().(bulkDoneMsg); len(msg.failed) != 0 || msg.what != "priority High" {
		t.Fatalf("%+v", msg)
	}
	if w := writes(); len(w) != 2 || !strings.HasSuffix(w[1], `{"fields":{"priority":{"id":"2"}}}`) {
		t.Errorf("writes = %q", w)
	}
}

// TestBulkLabels: +add -remove on every marked card, others untouched.
func TestBulkLabels(t *testing.T) {
	m, writes := bulkModel(t)
	m = pickBulk(t, m, "labels")
	if !m.jiraFieldActive || m.jiraFieldName != "bulk-labels" {
		t.Fatal("labels should open the input")
	}
	m.jiraFieldInput.SetValue("ui -old")
	out, cmd := m.applyJiraField()
	m = out.(Model)
	if msg := cmd().(bulkDoneMsg); len(msg.failed) != 0 {
		t.Fatal(msg.failed)
	}
	body := `{"update":{"labels":[{"add":"ui"},{"remove":"old"}]}}`
	if w := writes(); len(w) != 2 || w[0] != "PUT /rest/api/3/issue/ABC-1 "+body || w[1] != "PUT /rest/api/3/issue/ABC-3 "+body {
		t.Errorf("writes = %q", w)
	}
}

// TestBulkSprint: one move request for every marked card.
func TestBulkSprint(t *testing.T) {
	m, writes := bulkModel(t)
	m = pickBulk(t, m, "sprint")
	m.jiraPicker.idx = slices.IndexFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.label == "Backlog" })
	_, cmd := m.applyJiraPick()
	if msg := cmd().(jiraMutatedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if w := writes(); len(w) != 1 || !strings.Contains(w[0], `"issues":["ABC-1","ABC-3"]`) {
		t.Errorf("writes = %q", w)
	}
}

// TestMarkAll: X marks the lane's cards, X again unmarks them.
func TestMarkAll(t *testing.T) {
	m := jiraTabModel(t) // To do lane: ABC-1, ABC-3
	out, _ := m.handleJiraKey(keyMsg(t, "X"))
	m = out.(Model)
	if got := m.markedKeys(); !slices.Equal(got, []string{"ABC-1", "ABC-3"}) {
		t.Fatalf("marked = %v", got)
	}
	out, _ = m.handleJiraKey(keyMsg(t, "X"))
	if m = out.(Model); len(m.markedKeys()) != 0 {
		t.Errorf("second X should unmark, got %v", m.markedKeys())
	}
}

// TestBulkStatusForm: when the move needs fields, the form is asked once
// and its values go with every card's move.
func TestBulkStatusForm(t *testing.T) {
	m, writes := bulkModel(t)
	form := &jiraFormState{key: "ABC-1", to: "Done", bulk: []string{"ABC-1", "ABC-3"},
		fields: []jiraFormField{{FieldMeta: jira.FieldMeta{ID: "customfield_9", Name: "Resolution note", Kind: jira.KindText}, required: true}}}
	out, _ := m.handleBulkMove(bulkMoveMsg{keys: form.bulk, to: "Done", form: form})
	m = out.(Model)
	if m.jiraForm == nil || !strings.Contains(m.status, "for all 2 cards") {
		t.Fatalf("form not shown: %q", m.status)
	}
	m.jiraForm.fields[0].val.Text, m.jiraForm.fields[0].changed = "shipped", true
	cmd := m.submitJiraForm()
	if m.jiraForm != nil || cmd == nil {
		t.Fatal("submit should close the form and move all")
	}
	msg := cmd().(bulkDoneMsg)
	// ABC-3 has no move to Done in the fake; ABC-1 moves with the field.
	if len(msg.failed) != 1 {
		t.Errorf("failed = %v", msg.failed)
	}
	if w := writes(); len(w) != 1 || !strings.Contains(w[0], `"customfield_9":"shipped"`) {
		t.Errorf("writes = %q", w)
	}
}

// TestQuickEdit: e edits the selected card alone with the bulk editors,
// leaving the marks as they were.
func TestQuickEdit(t *testing.T) {
	m, writes := bulkModel(t) // ABC-1 and ABC-3 marked
	m.selectJiraKey("ABC-2")
	out, _ := m.handleJiraKey(keyMsg(t, "e"))
	m = out.(Model)
	if !m.jiraPicker.active || m.jiraPicker.title != "Edit ABC-2" || slices.ContainsFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == "clear" }) {
		t.Fatalf("picker %q %+v", m.jiraPicker.title, m.jiraPicker.items)
	}
	m.jiraPicker.idx = slices.IndexFunc(m.jiraPicker.items, func(it jiraPickerItem) bool { return it.id == "labels" })
	out, _ = m.applyJiraPick()
	m = out.(Model)
	m.jiraFieldInput.SetValue("ui")
	out, cmd := m.applyJiraField()
	m = out.(Model)
	out, _ = m.handleBulkDone(cmd().(bulkDoneMsg))
	m = out.(Model)
	if w := writes(); len(w) != 1 || !strings.HasPrefix(w[0], "PUT /rest/api/3/issue/ABC-2 ") {
		t.Errorf("writes = %q", w)
	}
	if len(m.jiraTab.marked) != 2 || m.quickKey != "" {
		t.Errorf("marks %v, quick %q", m.jiraTab.marked, m.quickKey)
	}
}
