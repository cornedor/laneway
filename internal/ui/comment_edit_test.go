package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestCommentEditShowsNewBody: after ctrl+s on an edited comment the panel
// shows the new body, not the old: a comment posted as plain text with
// markdown in it ("**") edits as its text, not as a placeholder that saves
// the old paragraph back.
func TestCommentEditShowsNewBody(t *testing.T) {
	body := "old **words**"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			fmt.Fprint(w, `{"accountId":"me1","displayName":"Me"}`)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/comment/2"):
			var in struct {
				Body json.RawMessage `json:"body"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &in)
			if strings.Contains(string(in.Body), `"new **words**"`) {
				body = "new **words**"
			}
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/ABC-1":
			fmt.Fprintf(w, `{"key":"ABC-1","fields":{"summary":"s","comment":{"total":1,"comments":[
				{"id":"2","author":{"accountId":"me1","displayName":"Me"},"created":"2025-09-25T09:00:00.000+0000",
				 "body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":%q}]}]}}]}}}`, body)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	m := configuredJiraModel(t, "ABC")
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	_, _ = m.jiraClient.Myself(context.Background())
	out, cmd := openRefFor(m, "ABC-1")
	msg, _ := findMsg[jiraLoadedMsg](cmd)
	out, _ = out.(Model).Update(msg)
	m = out.(Model)
	if !strings.Contains(ansi.Strip(m.refView.GetContent()), "old words") {
		t.Fatalf("no comment:\n%s", ansi.Strip(m.refView.GetContent()))
	}

	loaded := m.editComment(0)().(descLoadedMsg)
	out, _ = m.Update(loaded)
	m = out.(Model)
	if m.descEdit == nil {
		t.Fatal("no editor")
	}
	m.descEdit.input.SetValue(strings.Replace(m.descEdit.input.Value(), "old", "new", 1))
	out, cmd = m.handleKey(keyMsg(t, "ctrl+s"))
	m = out.(Model)
	mut, ok := findMsg[jiraMutatedMsg](cmd)
	if !ok || mut.err != nil {
		t.Fatalf("save = %+v", mut)
	}
	out, cmd = m.Update(mut)
	m = out.(Model)
	re, ok := findMsg[jiraLoadedMsg](cmd)
	if !ok {
		t.Fatal("no reload")
	}
	out, _ = m.Update(re)
	m = out.(Model)
	if c := ansi.Strip(m.refView.GetContent()); !strings.Contains(c, "new words") {
		t.Errorf("old comment still shown:\n%s", c)
	}
}
