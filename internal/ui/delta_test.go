package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

func TestMergeCards(t *testing.T) {
	cards := []jira.Card{{Key: "A-1", Summary: "one"}, {Key: "A-2", Summary: "two"}}
	got, added := mergeCards(cards, []jira.Card{{Key: "A-2", Summary: "TWO"}, {Key: "A-3"}})
	if added != 1 || len(got) != 3 || got[1].Summary != "TWO" || got[2].Key != "A-3" || cards[1].Summary != "two" {
		t.Errorf("got %+v, added %d", got, added)
	}
}

// TestDeltaRefresh: an idle refresh soon after a whole fetch asks only for
// what was updated and merges it; a stale one fetches the view whole.
func TestDeltaRefresh(t *testing.T) {
	var jql string
	gone := `{"issues":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost { // which loaded cards changed at all
			io.WriteString(w, gone)
			return
		}
		if q := r.URL.Query().Get("jql"); q != "" {
			jql = q
		}
		io.WriteString(w, `{"total":1,"issues":[{"key":"ABC-2","fields":{"summary":"Second, renamed","status":{"id":"3"}}}]}`)
	}))
	defer srv.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	tt := m.jiraTab
	if tt.fullAt.IsZero() {
		t.Fatal("the board load should count as a whole fetch")
	}
	tt.fetched = time.Now().Add(-3 * time.Minute)
	msg := m.loadJiraDelta()().(jiraCardsMsg)
	if !msg.delta || !strings.Contains(jql, "updated >= -5m") {
		t.Fatalf("delta %v, jql %q", msg.delta, jql)
	}
	out, _ := m.handleJiraCards(msg)
	m = out.(Model)
	i := slices.IndexFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == "ABC-2" })
	if len(m.jiraTab.cards) != 4 || m.jiraTab.cards[i].Summary != "Second, renamed" {
		t.Errorf("cards = %+v", m.jiraTab.cards)
	}

	// ABC-4 changed but is no longer in the view: it goes.
	gone = `{"issues":[{"key":"ABC-2","fields":{}},{"key":"ABC-4","fields":{}}]}`
	m.jiraTab.fullAt = time.Now()
	out, _ = m.handleJiraCards(m.loadJiraDelta()().(jiraCardsMsg))
	m = out.(Model)
	if slices.ContainsFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == "ABC-4" }) || len(m.jiraTab.cards) != 3 {
		t.Errorf("cards after ABC-4 left = %+v", m.jiraTab.cards)
	}

	// Past ui.full_refresh the view is fetched whole again.
	jql = ""
	m.jiraTab.fullAt = time.Now().Add(-m.opts.fullRefresh)
	if msg, ok := m.loadJiraDelta()().(jiraCardsMsg); !ok || msg.delta {
		t.Error("a stale whole fetch should be followed by a whole one")
	}
	if strings.Contains(jql, "updated >=") {
		t.Errorf("whole fetch jql = %q", jql)
	}
	m.jiraTab.fullAt = time.Now()
	m.jiraTab.quickOn = map[int]bool{7: true} // other filters: whole fetch
	if m.jiraFetchKey(m.jiraTab.viewIdx) == m.jiraTab.fullKey {
		t.Error("the filter should change the fetch key")
	}
}
