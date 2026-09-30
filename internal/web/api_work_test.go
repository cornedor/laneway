package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

func workServer(t *testing.T) *httptest.Server {
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
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, Jira: config.JiraConfig{}, Store: st, Site: "demo", Demo: true}))
	t.Cleanup(ts.Close)
	return ts
}

func workCall(t *testing.T, method, url string, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestWorkAndWorklogs(t *testing.T) {
	ts := workServer(t)
	var work struct{ Cards []jira.Card }
	if workCall(t, "GET", ts.URL+"/api/work", "", &work) != 200 || len(work.Cards) == 0 {
		t.Fatalf("my work = %+v", work)
	}
	key := work.Cards[0].Key
	today := time.Now().Format(time.DateOnly)
	tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	started := time.Now().Add(-time.Hour).Format(time.RFC3339)
	if c := workCall(t, "POST", ts.URL+"/api/worklog/"+key, `{"Seconds":3600,"Started":"`+started+`","Comment":"tests","Left":"keep"}`, nil); c != 200 {
		t.Fatalf("add worklog = %d", c)
	}
	var logs struct{ Worklogs []jira.Worklog }
	workCall(t, "GET", ts.URL+"/api/worklogs?from="+today+"&to="+tomorrow, "", &logs)
	if len(logs.Worklogs) == 0 {
		t.Fatal("logged work not listed")
	}
	if c := workCall(t, "POST", ts.URL+"/api/worklog/"+key, `{"Seconds":10}`, nil); c == 200 {
		t.Fatal("under a minute was accepted")
	}
	if c := workCall(t, "GET", ts.URL+"/api/worklogs?from=x&to=y", "", nil); c != 400 {
		t.Fatalf("bad days = %d", c)
	}
	w := logs.Worklogs[len(logs.Worklogs)-1]
	if c := workCall(t, "DELETE", ts.URL+"/api/worklog/"+w.Key+"/"+w.ID, "", nil); c != 200 {
		t.Fatalf("delete = %d", c)
	}
}

func TestInboxMarks(t *testing.T) {
	ts := workServer(t)
	var in struct {
		Threads []InboxThread
		Marks   map[string]InboxMark
		Floor   int64
	}
	if workCall(t, "GET", ts.URL+"/api/inbox", "", &in) != 200 || in.Floor == 0 {
		t.Fatalf("inbox = %+v", in)
	}
	now := time.Now().UnixMilli()
	if c := workCall(t, "PUT", ts.URL+"/api/inbox/state/DEMO-1", fmt.Sprintf(`{"Read":%d,"Done":%d}`, now, now+2), nil); c != 200 {
		t.Fatalf("put state = %d", c)
	}
	workCall(t, "GET", ts.URL+"/api/inbox", "", &in)
	if m := in.Marks["DEMO-1"]; m.Read != now || m.Done != now+2 {
		t.Fatalf("marks = %+v", in.Marks)
	}
}

func TestStandup(t *testing.T) {
	ts := workServer(t)
	var su struct {
		Entries []jira.InboxEntry
		Cards   []jira.Card
	}
	since := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)
	if c := workCall(t, "GET", ts.URL+"/api/standup?since="+since, "", &su); c != 200 {
		t.Fatalf("standup = %d", c)
	}
	var people []struct{ ID, Name string }
	if c := workCall(t, "GET", ts.URL+"/api/standup/people", "", &people); c != 200 {
		t.Fatalf("people = %d", c)
	}
}
