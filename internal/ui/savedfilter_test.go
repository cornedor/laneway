package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/jira"
)

func TestAndOrderedJQL(t *testing.T) {
	for _, c := range [][3]string{
		{"type = Bug ORDER BY created DESC", "assignee = currentUser()", "(type = Bug) AND (assignee = currentUser()) ORDER BY created DESC"},
		{"type = Bug", "", "type = Bug"},
		{"ORDER BY rank", "labels = x", "labels = x ORDER BY rank"},
		{"project = A order by key", "", "project = A order by key"},
	} {
		if got := andOrderedJQL(c[0], c[1]); got != c[2] {
			t.Errorf("andOrderedJQL(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// TestFilterView: a starred filter's view is its own search, narrowed by
// the board's filters, its order kept.
func TestFilterView(t *testing.T) {
	var jql string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			JQL string `json:"jql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		jql = body.JQL
		io.WriteString(w, `{"issues":[{"key":"X-1","fields":{"summary":"One"}}]}`)
	}))
	defer srv.Close()
	c := jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", StoryPointsField: "customfield_1"})
	v := jiraView{kind: jiraViewFilter, name: "My bugs", jql: "type = Bug ORDER BY created DESC"}
	cards, total, err := fetchJiraView(context.Background(), c, 1, &jira.BoardConfig{}, v, "labels = ui")
	if err != nil || len(cards) != 1 || total != 1 {
		t.Fatalf("%v %d %v", cards, total, err)
	}
	if jql != "(type = Bug) AND (labels = ui) ORDER BY created DESC" {
		t.Errorf("jql = %q", jql)
	}
}

func TestSavedFiltersOption(t *testing.T) {
	if o, _ := optionsFrom(config.UIConfig{}); !o.savedFilters {
		t.Error("saved filters should default on")
	}
	if o, _ := optionsFrom(config.UIConfig{SavedFilters: "off"}); o.savedFilters {
		t.Error("off should turn them off")
	}
	if _, warn := optionsFrom(config.UIConfig{SavedFilters: "maybe"}); len(warn) != 1 {
		t.Errorf("warn = %v", warn)
	}
}
