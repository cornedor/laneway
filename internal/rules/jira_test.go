package rules

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

func TestJiraActionsCompile(t *testing.T) {
	_, warn := Compile([]Rule{
		{Name: "t", Actions: []Action{{Type: "transition"}}},
		{Name: "c", Actions: []Action{{Type: "comment"}}},
		{Name: "ok", Actions: []Action{{Type: "transition", To: "Done"}, {Type: "comment", Text: "Moved {{.Key}}"}}},
	})
	if len(warn) != 2 || !strings.Contains(warn[0], "needs to:") || !strings.Contains(warn[1], "needs text") {
		t.Errorf("warn = %q", warn)
	}
}

// TestJiraActionsOnlyOnOthers: a Jira action fires only on a change the
// changelog says someone else made — never on yours, never unknown.
func TestJiraActionsOnlyOnOthers(t *testing.T) {
	set, _ := Compile([]Rule{{Name: "r", Actions: []Action{{Type: "log"}, {Type: "comment", Text: "hi {{.Key}}"}}}})
	if !set.UsesByMe() {
		t.Error("a Jira action needs the change's author")
	}
	ev := Event{Kind: Status, Card: jira.Card{Key: "A-1"}}
	yes, no := true, false
	for _, c := range []struct {
		byMe *bool
		want int
	}{{nil, 1}, {&yes, 1}, {&no, 2}} {
		ev.ByMe = c.byMe
		if got := set.Fire(ev); len(got) != c.want {
			t.Errorf("byMe %v: %d firings, want %d", c.byMe, len(got), c.want)
		}
	}
	if got := set.Fire(ev); got[1].Text != "hi A-1" {
		t.Errorf("comment text = %q", got[1].Text)
	}
}

func TestJiraAct(t *testing.T) {
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"transitions":[{"id":"31","to":{"name":"Done"}}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		writes = append(writes, r.Method+" "+r.URL.Path+" "+string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	ctx := context.Background()
	vars := map[string]string{"Key": "A-1", "Status": "In review"}
	if err := JiraAct(ctx, c, Firing{Action: "transition", To: "done", Vars: vars}); err != nil {
		t.Fatal(err)
	}
	if err := JiraAct(ctx, c, Firing{Action: "transition", To: "Nowhere", Vars: vars}); err == nil || !strings.Contains(err.Error(), "no move to Nowhere") {
		t.Errorf("err = %v", err)
	}
	vars["Status"] = "Done"
	if err := JiraAct(ctx, c, Firing{Action: "transition", To: "Done", Vars: vars}); err != nil {
		t.Fatal(err)
	}
	if err := JiraAct(ctx, c, Firing{Action: "comment", Text: "moved", Vars: vars}); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || !strings.Contains(writes[0], `/A-1/transitions {"transition":{"id":"31"}}`) || !strings.Contains(writes[1], "/A-1/comment") {
		t.Errorf("writes = %q", writes)
	}
}
