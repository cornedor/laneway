package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
)

// fakeJira answers searches from issues (key → summary), two to a page, and
// keeps the JQL it was asked.
type fakeJira struct {
	mu     sync.Mutex
	issues map[string]string
	// match narrows a search by its JQL; nil finds every issue.
	match func(jql, key string) bool
	asked []string
}

func (f *fakeJira) serve(t *testing.T) *jira.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/rest/api/3/field":
			fmt.Fprint(w, `[]`)
			return
		case "/rest/api/3/search/approximate-count":
			fmt.Fprintf(w, `{"count": %d}`, len(f.issues))
			return
		}
		var body struct {
			JQL   string `json:"jql"`
			Token string `json:"nextPageToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.asked = append(f.asked, body.JQL)
		var keys []string
		for k := range f.issues {
			if f.match == nil || f.match(body.JQL, k) {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		from := 0
		fmt.Sscan(body.Token, &from)
		to := min(from+2, len(keys))
		var out []map[string]any
		for _, k := range keys[from:to] {
			out = append(out, map[string]any{"key": k, "fields": map[string]any{"summary": f.issues[k]}})
		}
		resp := map[string]any{"issues": out}
		if to < len(keys) {
			resp["nextPageToken"] = fmt.Sprint(to)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
}

func openIndex(t *testing.T) *index.Index {
	ix, err := index.Open(filepath.Join(t.TempDir(), "ix.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

// TestRunMirrorsWholeProject: a run reads every page, drops what the project
// no longer has, and leaves the mirror complete.
func TestRunMirrorsWholeProject(t *testing.T) {
	ctx := context.Background()
	f := &fakeJira{issues: map[string]string{"ABC-1": "one", "ABC-2": "two", "ABC-3": "three"}}
	c, ix := f.serve(t), openIndex(t)
	ix.PutCards([]jira.Card{{Key: "ABC-9", Summary: "deleted since"}, {Key: "XY-1", Summary: "another project"}})
	time.Sleep(2 * time.Millisecond)
	r, err := Start(ctx, c, ix, "ABC")
	if err != nil || r.Total != 3 {
		t.Fatalf("Start = %+v, %v", r, err)
	}
	pages := 0
	for !r.Finished() {
		if err := r.Next(ctx, c, ix); err != nil {
			t.Fatal(err)
		}
		if pages++; pages == 1 && ix.ProjectSync("ABC").Complete {
			t.Error("complete after the first of two pages")
		}
	}
	if pages != 2 || r.Done != 3 {
		t.Errorf("pages %d, done %d; want 2 and 3", pages, r.Done)
	}
	if !strings.HasPrefix(f.asked[0], `project = "ABC"`) || !strings.Contains(f.asked[0], "ORDER BY created DESC") {
		t.Errorf("asked %q", f.asked[0])
	}
	cards, _ := ix.ProjectCards("ABC")
	if len(cards) != 3 || slices.ContainsFunc(cards, func(c jira.Card) bool { return c.Key == "ABC-9" }) {
		t.Errorf("mirror = %+v; want ABC-1..3, ABC-9 dropped", cards)
	}
	if _, ok := ix.Get("XY-1"); !ok {
		t.Error("another project's issue was dropped")
	}
	if s := ix.ProjectSync("ABC"); !s.Complete || !s.At.Equal(r.Started.Truncate(time.Millisecond)) {
		t.Errorf("sync = %+v", s)
	}
}

// TestRefreshReadsChangesAndDropsGone: a refresh asks only what changed since
// the last sync, and past keysEvery drops keys Jira no longer has.
func TestRefreshReadsChangesAndDropsGone(t *testing.T) {
	ctx := context.Background()
	f := &fakeJira{issues: map[string]string{"ABC-1": "renamed", "ABC-3": "new"}}
	c, ix := f.serve(t), openIndex(t)
	if err := Refresh(ctx, c, ix, "ABC"); err != nil || len(f.asked) != 0 {
		t.Fatalf("an incomplete mirror: Refresh asked %q, %v; want nothing", f.asked, err)
	}
	ix.PutCards([]jira.Card{{Key: "ABC-1", Summary: "one"}, {Key: "ABC-2", Summary: "deleted"}})
	last := time.Now().Add(-10 * time.Minute)
	_ = ix.SetProjectSync("ABC", index.ProjectSync{At: last, KeysAt: last, Complete: true})
	if err := Refresh(ctx, c, ix, "ABC"); err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`updated >= -(\d+)m`).FindStringSubmatch(f.asked[0]); m == nil || m[1] != "13" {
		t.Errorf("asked %q; want the 10 minutes since, 2 overlap and 1 rounding", f.asked[0])
	}
	if len(f.asked) != 1 {
		t.Errorf("asked %q; want the changes only, the keys within the hour", f.asked)
	}
	if got, _ := ix.Get("ABC-1"); got.Summary != "renamed" {
		t.Errorf("ABC-1 = %+v; want the change read", got)
	}
	if _, ok := ix.Get("ABC-2"); !ok {
		t.Error("ABC-2 dropped without a key pass")
	}
	_ = ix.SetProjectSync("ABC", index.ProjectSync{At: last, KeysAt: last.Add(-2 * time.Hour), Complete: true})
	f.asked = nil
	if err := Refresh(ctx, c, ix, "ABC"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Get("ABC-2"); ok {
		t.Errorf("ABC-2 kept after a key pass; asked %q", f.asked)
	}
	if s := ix.ProjectSync("ABC"); time.Since(s.At) > time.Minute || time.Since(s.KeysAt) > time.Minute {
		t.Errorf("sync not moved on: %+v", s)
	}
}

// TestCardsFilterByJiraKeys: a filter narrows the mirror to the keys Jira
// finds for it within the project.
func TestCardsFilterByJiraKeys(t *testing.T) {
	ctx := context.Background()
	f := &fakeJira{issues: map[string]string{"ABC-2": "mine"}}
	c, ix := f.serve(t), openIndex(t)
	ix.PutCards([]jira.Card{{Key: "ABC-1"}, {Key: "ABC-2"}})
	all, note, err := Cards(ctx, c, ix, "ABC", "", "")
	if err != nil || len(all) != 2 || note != "" || len(f.asked) != 0 {
		t.Fatalf("no filter: %d cards, %q, %v, asked %q", len(all), note, err, f.asked)
	}
	got, _, err := Cards(ctx, c, ix, "ABC", "assignee = currentUser()", "")
	if err != nil || len(got) != 1 || got[0].Key != "ABC-2" {
		t.Fatalf("filtered = %+v, %v", got, err)
	}
	if f.asked[0] != `project = "ABC" AND (assignee = currentUser())` {
		t.Errorf("asked %q", f.asked[0])
	}
}
