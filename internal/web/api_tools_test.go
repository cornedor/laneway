package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/offline"
	"github.com/cornedor/laneway/internal/rules"
	"github.com/cornedor/laneway/internal/store"
)

func toolsServer(t *testing.T, mod func(*Options)) (*httptest.Server, Options) {
	t.Helper()
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	opt := Options{Client: cl, Store: st, Site: "demo", Demo: true}
	if mod != nil {
		mod(&opt)
	}
	ts := httptest.NewServer(New(context.Background(), opt))
	t.Cleanup(ts.Close)
	return ts, opt
}

func TestRulesAPI(t *testing.T) {
	ts, _ := toolsServer(t, func(o *Options) {
		o.Rules = []rules.Rule{
			{Name: "done", On: rules.StrList{"status"}, Match: rules.Match{Status: rules.StrList{"Done"}}, Actions: []rules.Action{{Type: "log"}, {Type: "notify", Title: "Hi"}}},
			{Name: "bugs", Match: rules.Match{Type: rules.StrList{"Bug"}}, Actions: []rules.Action{{Type: "log"}}},
			{Name: "broken", Match: rules.Match{Summary: "("}, Actions: []rules.Action{{Type: "log"}}},
		}
	})
	var list struct {
		Rules    []rules.Rule
		Warnings []string
	}
	if workCall(t, "GET", ts.URL+"/api/rules", "", &list) != 200 || len(list.Rules) != 2 || len(list.Warnings) != 1 {
		t.Fatalf("rules = %+v", list)
	}
	var res []struct {
		Rule    string
		Fires   bool
		Why     string
		Actions []struct{ Type, Text string }
	}
	body := `{"On":"status","Key":"DEMO-1","Type":"Task","Status":"Done","FromStatus":"To Do"}`
	if workCall(t, "POST", ts.URL+"/api/rules/test", body, &res) != 200 || len(res) != 2 {
		t.Fatalf("test = %+v", res)
	}
	if !res[0].Fires || len(res[0].Actions) != 2 || res[1].Fires || !strings.HasPrefix(res[1].Why, "type") {
		t.Errorf("test = %+v", res)
	}
}

func TestNotesShareTUIFiles(t *testing.T) {
	ts, opt := toolsServer(t, nil)
	var n struct{ Text string }
	if workCall(t, "PUT", ts.URL+"/api/issues/DEMO-1/notes", `{"Text":"  remember\nthis "}`, nil) != 200 {
		t.Fatal("put")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(opt.Store.Path()), "notes", "DEMO-1.md"))
	if err != nil || string(raw) != "remember\nthis\n" {
		t.Fatalf("file = %q, %v", raw, err)
	}
	workCall(t, "GET", ts.URL+"/api/issues/DEMO-1/notes", "", &n)
	if n.Text != "remember\nthis" {
		t.Errorf("get = %q", n.Text)
	}
	var keys []string
	workCall(t, "GET", ts.URL+"/api/notes", "", &keys)
	if len(keys) != 1 || keys[0] != "DEMO-1" {
		t.Errorf("keys = %v", keys)
	}
	workCall(t, "PUT", ts.URL+"/api/issues/DEMO-1/notes", `{"Text":" "}`, nil)
	if _, err := os.Stat(filepath.Join(filepath.Dir(opt.Store.Path()), "notes", "DEMO-1.md")); !os.IsNotExist(err) {
		t.Error("empty notes stay")
	}
	if workCall(t, "GET", ts.URL+"/api/issues/bad/notes", "", nil) != 400 {
		t.Error("bad key accepted")
	}
}

func TestUserActionsRun(t *testing.T) {
	ts, _ := toolsServer(t, func(o *Options) {
		o.UI.Actions = []config.Action{
			{Name: "broken"},
			{Name: "echo", Key: "Z", Command: []string{"sh", "-c", "cat; echo; echo hi $LANEWAY_KEY"}, Refresh: true},
		}
	})
	var list []struct {
		ID   int
		Name string
	}
	workCall(t, "GET", ts.URL+"/api/actions", "", &list)
	if len(list) != 1 || list[0].ID != 1 {
		t.Fatalf("actions = %+v", list)
	}
	var out struct {
		Output, Error string
		Refresh       bool
	}
	if workCall(t, "POST", ts.URL+"/api/actions/1/run", `{"Keys":["DEMO-1"]}`, &out) != 200 || !strings.Contains(out.Output, `"key":"DEMO-1"`) || !strings.HasSuffix(out.Output, "hi DEMO-1") || !out.Refresh {
		t.Fatalf("run = %+v", out)
	}
	if workCall(t, "POST", ts.URL+"/api/actions/0/run", `{"Keys":["DEMO-1"]}`, nil) != 404 {
		t.Error("ran an unusable action")
	}
	if workCall(t, "POST", ts.URL+"/api/actions/1/run", `{"Keys":[]}`, nil) != 400 {
		t.Error("ran on nothing")
	}
}

func TestAskStreams(t *testing.T) {
	script := filepath.Join(t.TempDir(), "llm.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\nhead -n1 >/dev/null\necho \"Q: $1\"\n"), 0o700)
	ts, _ := toolsServer(t, func(o *Options) { o.UI.LLM = script })
	res, err := http.Post(ts.URL+"/api/issues/DEMO-1/ask", "application/json", bytes.NewReader([]byte(`{"Question":"points"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("type %q", res.Header.Get("Content-Type"))
	}
	var got strings.Builder
	sawDone := false
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: \"") {
			var s string
			_ = json.Unmarshal([]byte(line[6:]), &s)
			got.WriteString(s)
		}
		if line == "event: done" {
			sawDone = true
		}
	}
	if !sawDone || !strings.Contains(got.String(), "Q: Suggest story points") {
		t.Errorf("answer %q done=%v", got.String(), sawDone)
	}
	if workCall(t, "POST", ts.URL+"/api/issues/DEMO-1/ask", `{}`, nil) != 400 {
		t.Error("empty question accepted")
	}
}

func TestQueueAPI(t *testing.T) {
	ts, opt := toolsServer(t, nil)
	offline.To(opt.Store)(jira.PendingWrite{Method: "PUT", Path: "/rest/api/3/issue/DEMO-1", What: "summary", At: time.Now()})
	offline.To(opt.Store)(jira.PendingWrite{Method: "PUT", Path: "/rest/api/3/issue/DEMO-2", What: "summary", At: time.Now()})
	var q []struct {
		Index int
		Key   string
	}
	workCall(t, "GET", ts.URL+"/api/queue", "", &q)
	if len(q) != 2 || q[1].Key != "DEMO-2" {
		t.Fatalf("queue = %+v", q)
	}
	var left struct{ Left int }
	if workCall(t, "DELETE", ts.URL+"/api/queue/0", "", &left) != 200 || left.Left != 1 {
		t.Errorf("drop = %+v", left)
	}
}
