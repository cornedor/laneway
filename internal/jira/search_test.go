package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSearchCards: a JQL search pages by nextPageToken and reads points from
// the first story point field an issue fills.
func TestSearchCards(t *testing.T) {
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			fmt.Fprint(w, `[{"id":"customfield_1","name":"Story Points"},{"id":"customfield_2","name":"Story point estimate"}]`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body struct {
			JQL   string `json:"jql"`
			Token string `json:"nextPageToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.JQL != "assignee = currentUser()" {
			t.Errorf("jql = %q", body.JQL)
		}
		tokens = append(tokens, body.Token)
		if body.Token == "" {
			fmt.Fprint(w, `{"issues": [{"key": "ABC-1", "fields": {"summary": "One", "status": {"id": "3", "name": "Done"}, "customfield_2": 3}}], "nextPageToken": "p2"}`)
			return
		}
		fmt.Fprint(w, `{"issues": [{"key": "ABC-2", "fields": {"summary": "Two", "customfield_1": null}}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cards, err := c.SearchCards(context.Background(), "assignee = currentUser()")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Key != "ABC-1" || cards[0].Status != "Done" || cards[0].Points != "3" || cards[1].Points != "" {
		t.Errorf("cards = %+v", cards)
	}
	if len(tokens) != 2 || tokens[1] != "p2" {
		t.Errorf("tokens = %q", tokens)
	}
}
