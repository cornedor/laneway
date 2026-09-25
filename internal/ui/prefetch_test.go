package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

func TestNeighbourKeys(t *testing.T) {
	m := jiraTabModel(t)
	out, _ := m.handleJiraKey(keyMsg(t, "t")) // list mode
	m = out.(Model)
	t0 := m.jiraTab
	t0.idx = 1
	key := func(i int) string { return t0.cards[t0.order[i]].Key }
	if got, want := m.jiraNeighbourKeys(2), []string{key(1), key(2), key(0), key(3)}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestPrefetchOnRest: moving the cursor arms a prefetch; only the latest
// one runs, loading the cards around the cursor.
func TestPrefetchOnRest(t *testing.T) {
	var mu sync.Mutex
	var fetched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/rest/api/3/issue/")
		mu.Lock()
		fetched = append(fetched, key)
		mu.Unlock()
		io.WriteString(w, `{"key":"`+key+`","fields":{}}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", StoryPointsField: "customfield_1"})
	out, cmd := m.handleKey(keyMsg(t, "j"))
	m = out.(Model)
	if cmd == nil || m.prefetchSeq != 1 {
		t.Fatalf("no prefetch armed (seq %d)", m.prefetchSeq)
	}
	if _, cmd := m.handlePrefetch(prefetchMsg{seq: 0}); cmd != nil {
		t.Error("a stale prefetch should not run")
	}
	_, cmd = m.handlePrefetch(prefetchMsg{seq: 1})
	cmd()
	slices.Sort(fetched)
	if !slices.Equal(fetched, []string{"ABC-1", "ABC-3"}) {
		t.Errorf("fetched = %v", fetched)
	}
}
