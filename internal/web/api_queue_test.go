package web

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/offline"
)

type queueOut struct {
	Items []struct {
		ID, Key string
	}
	Failed []struct {
		ID    int
		What  string
		Error string
	}
	Conflict string
}

func TestQueueFailureSurfaced(t *testing.T) {
	ts, opt := toolsServer(t, nil)
	offline.To(opt.Store)(jira.PendingWrite{Method: "PUT", Path: "/rest/api/3/issue/NOPE-99", What: "summary", Body: json.RawMessage(`{}`), At: time.Now()})
	var res struct{ Failed []string }
	workCall(t, "POST", ts.URL+"/api/queue/send", `{"Force":true}`, &res)
	if len(res.Failed) != 1 {
		t.Fatalf("send = %+v", res)
	}
	var q queueOut
	workCall(t, "GET", ts.URL+"/api/queue", "", &q)
	if len(q.Items) != 0 || len(q.Failed) != 1 || q.Failed[0].Error == "" || q.Failed[0].What != "summary" {
		t.Fatalf("queue = %+v", q)
	}
	var ev []RuleEvent
	workCall(t, "GET", ts.URL+"/api/rules/events", "", &ev)
	if len(ev) != 1 || ev[0].Action != "queue" || ev[0].Err == "" {
		t.Errorf("events = %+v", ev)
	}
}

func TestQueueDropByID(t *testing.T) {
	ts, opt := toolsServer(t, nil)
	add := offline.To(opt.Store)
	now := time.Now()
	add(jira.PendingWrite{Method: "PUT", Path: "/rest/api/3/issue/DEMO-1", What: "a", At: now})
	add(jira.PendingWrite{Method: "PUT", Path: "/rest/api/3/issue/DEMO-2", What: "b", At: now.Add(time.Second)})
	var q queueOut
	workCall(t, "GET", ts.URL+"/api/queue", "", &q)
	second := q.Items[1].ID
	// the replayer sends the first one before the drop arrives
	offline.Write(opt.Store, offline.Read(opt.Store)[1:])
	var left struct{ Left int }
	if code := workCall(t, "DELETE", ts.URL+"/api/queue/"+second, "", &left); code != 200 || left.Left != 0 {
		t.Errorf("drop = %d %+v", code, left)
	}
	add(jira.PendingWrite{Method: "PUT", Path: "/rest/api/3/issue/DEMO-3", What: "c", At: now.Add(2 * time.Second)})
	if code := workCall(t, "DELETE", ts.URL+"/api/queue/"+second, "", &left); code != 200 || left.Left != 1 {
		t.Errorf("stale id dropped something else: %d %+v", code, left)
	}
}

func TestPrevCacheBounded(t *testing.T) {
	c := newPrevCache(3)
	for i := range 10 {
		c.put(fmt.Sprint(i), nil)
	}
	if c.len() != 3 {
		t.Fatalf("len = %d", c.len())
	}
	if _, ok := c.get("9"); !ok {
		t.Error("newest evicted")
	}
	if _, ok := c.get("0"); ok {
		t.Error("oldest kept")
	}
}

func TestRulesEmptyLists(t *testing.T) {
	ts, _ := toolsServer(t, nil)
	var raw map[string]json.RawMessage
	workCall(t, "GET", ts.URL+"/api/rules", "", &raw)
	for _, k := range []string{"Rules", "Watches", "Warnings", "Kinds"} {
		if string(raw[k]) == "null" || raw[k] == nil {
			t.Errorf("%s = %s", k, raw[k])
		}
	}
}
