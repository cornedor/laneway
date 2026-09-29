package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakePeople is an Indexer keeping People too: one project's.
type fakePeople struct {
	fakeIndex
	synced time.Time
	users  []User
	put    []User
}

func (f *fakePeople) PutUsers(_ string, us []User, _ bool) { f.put = append(f.put, us...) }
func (f *fakePeople) SyncAssignable(_ string, us []User)   { f.users, f.synced = us, time.Now() }
func (f *fakePeople) UsersSynced(string) time.Time         { return f.synced }
func (f *fakePeople) Users(_, query string, _ bool) []User {
	var out []User
	for _, u := range f.users {
		if strings.HasPrefix(strings.ToLower(u.DisplayName), strings.ToLower(query)) {
			out = append(out, u)
		}
	}
	return out
}

// TestAssignableUsersCached: the first search reads the project's people
// in full, once; later ones answer from them; a query they don't match
// asks Jira, and what it finds is kept.
func TestAssignableUsersCached(t *testing.T) {
	var full, searched int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("project") == "ABC" && q.Get("maxResults") == "1000":
			full++
			fmt.Fprint(w, `[{"accountId":"c1","displayName":"Claude"},{"accountId":"x1","displayName":"Gone","active":false}]`)
		case q.Get("query") != "":
			searched++
			fmt.Fprint(w, `[{"accountId":"n1","displayName":"Newcomer"}]`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	ix := &fakePeople{}
	c.SetIndex(ix)
	ctx := context.Background()
	for range 2 {
		us, err := c.AssignableUsers(ctx, "ABC-1", "cl")
		if err != nil || len(us) != 1 || us[0].DisplayName != "Claude" {
			t.Fatalf("cl = %v, %v", us, err)
		}
	}
	if full != 1 || searched != 0 {
		t.Fatalf("full reads %d, searches %d; want 1, 0", full, searched)
	}
	if len(ix.users) != 1 {
		t.Errorf("kept %v; want the inactive left out", ix.users)
	}
	us, err := c.AssignableUsers(ctx, "ABC-1", "new")
	if err != nil || len(us) != 1 || us[0].DisplayName != "Newcomer" || searched != 1 {
		t.Fatalf("new = %v, %v (searches %d)", us, err, searched)
	}
	if len(ix.put) != 1 || ix.put[0].AccountID != "n1" {
		t.Errorf("put %v; want the newcomer kept", ix.put)
	}
}

// TestAssignableUsersOffline: without Jira, the people kept answer, stale
// or not.
func TestAssignableUsersOffline(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", Email: "me@x.test", APIToken: "tok"})
	ix := &fakePeople{users: []User{{AccountID: "c1", DisplayName: "Claude"}}}
	c.SetIndex(ix)
	us, err := c.AssignableUsers(context.Background(), "ABC-1", "cl")
	if err != nil || len(us) != 1 || us[0].AccountID != "c1" {
		t.Fatalf("offline = %v, %v", us, err)
	}
}
