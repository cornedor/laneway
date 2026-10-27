package ui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/people"
	"github.com/cornedor/laneway/internal/store"
)

// A person picker lists who was picked in the field first, saying why, and
// counts a pick.
func TestPeopleRanked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/user/assignable/search":
			_, _ = w.Write([]byte(`[{"accountId":"a1","displayName":"Ada Lovelace"},{"accountId":"a2","displayName":"Alan Turing"},{"accountId":"a3","displayName":"Grace Hopper"}]`))
		case "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"me-1","displayName":"Me"}`))
		}
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := loadedJiraModel(t)
	m.store = st
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	key := m.jiraIssue.Key
	_ = people.Record(st, people.Project(key), "assignee", "a2", time.Now())

	updated, _ := m.handleJiraPickerLoaded(m.openJiraAssigneePicker()().(jiraPickerLoadedMsg))
	m = updated.(Model)
	first := m.jiraPicker.items[0]
	if first.id != "a2" || first.hint != "picked often" {
		t.Fatalf("first row = %+v, want Alan, picked often", first)
	}
	if it := m.jiraPicker.items[m.jiraPicker.idx]; it.id != "me-1" {
		t.Errorf("cursor on %q, want Assign to me", it.label)
	}

	m.jiraPicker.idx = 0
	m.applyJiraPick()
	if got := people.Picked(st, people.Project(key), "assignee", time.Now()); got["a2"] < 1.99 {
		t.Errorf("the pick was not counted: %v", got)
	}
}
