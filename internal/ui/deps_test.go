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

// TestDependencies: the tree draws both sides; enter opens a row's issue.
func TestDependencies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		links := `[]`
		if strings.HasSuffix(r.URL.Path, "/ABC-1") {
			links = `[{"type":{"name":"Blocks","outward":"blocks"},"inwardIssue":{"key":"ABC-2","fields":{"summary":"Second","status":{"name":"To Do"}}}}]`
		}
		key := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		io.WriteString(w, `{"key":"`+key+`","fields":{"summary":"S","status":{"name":"Open","statusCategory":{"key":"new"}},"issuelinks":`+links+`}}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	out, _ := m.handleJiraPickerLoaded(m.applyIssueAction("ABC-1", "deps")().(jiraPickerLoadedMsg))
	m = out.(Model)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"held up by 1 open issue", "held up by", "└ ABC-2 Second [To Do]", "holds up: nothing"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
	m.jiraPicker.idx = 2
	out, _ = m.applyJiraPick()
	if m = out.(Model); !m.refOpen || m.refs[m.refIdx].jiraKey != "ABC-2" {
		t.Error("enter should open ABC-2")
	}
}
