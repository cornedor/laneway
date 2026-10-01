package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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
	start := time.Now().Add(-time.Hour)
	from, started := start.Format(time.DateOnly), start.Format(time.RFC3339)
	tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	if c := workCall(t, "POST", ts.URL+"/api/worklog/"+key, `{"Seconds":3600,"Started":"`+started+`","Comment":"tests","Left":"keep"}`, nil); c != 200 {
		t.Fatalf("add worklog = %d", c)
	}
	var logs struct{ Worklogs []jira.Worklog }
	workCall(t, "GET", ts.URL+"/api/worklogs?from="+from+"&to="+tomorrow, "", &logs)
	if len(logs.Worklogs) == 0 {
		t.Fatal("logged work not listed")
	}
	var issueLogs []jira.Worklog
	if c := workCall(t, "GET", ts.URL+"/api/issues/"+key+"/worklogs", "", &issueLogs); c != 200 || len(issueLogs) == 0 || issueLogs[len(issueLogs)-1].Seconds != 3600 {
		t.Fatalf("issue worklogs = %d %+v", c, issueLogs)
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
	if c := workCall(t, "PUT", ts.URL+"/api/inbox/state/DEMO-2", `{"Site":"elsewhere","Read":1}`, nil); c != 400 {
		t.Fatalf("put on an unknown site = %d", c)
	}
	workCall(t, "GET", ts.URL+"/api/inbox", "", &in)
	if m := in.Marks["demo/DEMO-1"]; m.Read != now || m.Done != now+2 {
		t.Fatalf("marks = %+v", in.Marks)
	}
}

// The inbox reads every configured site, marks each thread under its own
// site and keeps the unread count for laneway prompt.
func TestInboxEverySite(t *testing.T) {
	site := func(name string) Options {
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
		return Options{Client: cl, Store: st, Site: name}
	}
	opt, other := site(""), site("work")
	opt.Sites = []string{"", "work"}
	opt.Open = func(name string) (Options, error) { return other, nil }
	ts := httptest.NewServer(New(context.Background(), opt))
	t.Cleanup(ts.Close)

	var in struct {
		Threads []InboxThread
		Site    string
	}
	if c := workCall(t, "GET", ts.URL+"/api/inbox", "", &in); c != 200 || len(in.Threads) == 0 {
		t.Fatalf("inbox = %d %+v", c, in)
	}
	sites := map[string]int{}
	for _, th := range in.Threads {
		sites[th.Site]++
		if th.ID != th.Site+"/"+th.Key || th.URL == "" {
			t.Fatalf("thread = %+v", th)
		}
	}
	if sites[""] == 0 || sites["work"] == 0 {
		t.Fatalf("threads by site = %v", sites)
	}
	unread := func() string {
		v, _, _ := opt.Store.GetMeta(inboxUnreadMeta)
		return v
	}
	th := in.Threads[0]
	put := func(read int64) {
		t.Helper()
		if c := workCall(t, "PUT", ts.URL+"/api/inbox/state/"+th.Key, fmt.Sprintf(`{"Site":%q,"Read":%d}`, th.Site, read), nil); c != 200 {
			t.Fatalf("put state = %d", c)
		}
	}
	put(1) // all of it unread
	before, _ := strconv.Atoi(unread())
	put(th.Entries[len(th.Entries)-1].When.UnixMilli())
	if m := readMarks(&Server{opt: opt}); m[th.ID].R == 0 {
		t.Fatalf("marks = %v", m)
	}
	if unread() != strconv.Itoa(before-1) {
		t.Fatalf("inbox_unread = %s, was %d", unread(), before)
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
