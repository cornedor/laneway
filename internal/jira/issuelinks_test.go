package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeJira answers GETs from gets by path and records every other request.
func fakeJira(t *testing.T, gets map[string]string) (*Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			body, ok := gets[r.URL.Path]
			if !ok {
				t.Errorf("unexpected GET %s", r.URL)
			}
			io.WriteString(w, body)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue" {
			io.WriteString(w, `{"key":"ABC-9"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	return c, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), writes...) }
}

func TestLinkIssues(t *testing.T) {
	c, writes := fakeJira(t, nil)
	if err := c.LinkIssues(context.Background(), "Blocks", "ABC-1", "ABC-2"); err != nil {
		t.Fatal(err)
	}
	var body map[string]map[string]string
	_ = json.Unmarshal([]byte(strings.SplitN(writes()[0], " ", 3)[2]), &body)
	// ABC-1 blocks ABC-2: the blocker goes in inwardIssue.
	if body["inwardIssue"]["key"] != "ABC-1" || body["outwardIssue"]["key"] != "ABC-2" || body["type"]["name"] != "Blocks" {
		t.Errorf("body = %v", body)
	}
}

func TestToggleWatch(t *testing.T) {
	c, writes := fakeJira(t, map[string]string{
		"/rest/api/3/myself":               `{"accountId":"me"}`,
		"/rest/api/3/issue/ABC-1/watchers": `{"isWatching":true}`,
	})
	on, err := c.ToggleWatch(context.Background(), "ABC-1")
	if err != nil || on {
		t.Fatalf("on %v, %v", on, err)
	}
	if w := writes(); len(w) != 1 || !strings.HasPrefix(w[0], "DELETE /rest/api/3/issue/ABC-1/watchers?accountId=me") {
		t.Errorf("writes = %q", w)
	}
}

// TestClone: the copy carries the fields and is linked as a clone.
func TestClone(t *testing.T) {
	c, writes := fakeJira(t, map[string]string{
		"/rest/api/3/issue/ABC-1": `{"fields":{"issuetype":{"name":"Story"},"summary":"Pay","labels":["ui"],
		  "priority":{"id":"2"},"parent":{"key":"ABC-5"},"description":{"type":"doc","content":[]}}}`,
		"/rest/api/3/issueLinkType": `{"issueLinkTypes":[{"name":"Blocks"},{"name":"Cloners","inward":"is cloned by","outward":"clones"}]}`,
	})
	key, err := c.Clone(context.Background(), "ABC-1")
	if err != nil || key != "ABC-9" {
		t.Fatalf("key %q, %v", key, err)
	}
	w := writes()
	if len(w) != 2 {
		t.Fatalf("writes = %q", w)
	}
	for _, want := range []string{`"summary":"CLONE - Pay"`, `"parent":{"key":"ABC-5"}`, `"priority":{"id":"2"}`, `"labels":["ui"]`, `"project":{"key":"ABC"}`, `"description":{"type":"doc"`} {
		if !strings.Contains(w[0], want) {
			t.Errorf("create lacks %s: %s", want, w[0])
		}
	}
	if !strings.Contains(w[1], `"inwardIssue":{"key":"ABC-9"}`) || !strings.Contains(w[1], `"Cloners"`) {
		t.Errorf("link = %s", w[1])
	}
}

func TestDeleteLinkAndVote(t *testing.T) {
	c, writes := fakeJira(t, map[string]string{"/rest/api/3/issue/ABC-1/votes": `{"hasVoted":false}`})
	if err := c.DeleteLink(context.Background(), "ABC-1", "10200"); err != nil {
		t.Fatal(err)
	}
	on, err := c.ToggleVote(context.Background(), "ABC-1")
	if err != nil || !on {
		t.Fatalf("vote %v %v", on, err)
	}
	w := writes()
	if len(w) != 2 || !strings.HasPrefix(w[0], "DELETE /rest/api/3/issueLink/10200") || !strings.HasPrefix(w[1], "POST /rest/api/3/issue/ABC-1/votes") {
		t.Errorf("writes = %q", w)
	}
}
