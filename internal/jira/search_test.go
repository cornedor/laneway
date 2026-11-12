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

// TestSearchCardsDropsBadKeys: a key that could climb out of a folder never
// becomes a card.
func TestSearchCardsDropsBadKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			fmt.Fprint(w, `[]`)
			return
		}
		fmt.Fprint(w, `{"issues": [{"key": "../../x-1", "fields": {}}, {"key": "A/B-1", "fields": {}}, {"key": "ABC-1", "fields": {}}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cards, err := c.SearchCards(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].Key != "ABC-1" {
		t.Errorf("cards = %+v", cards)
	}
}

func TestValidKey(t *testing.T) {
	for key, want := range map[string]bool{"ABC-1": true, "A_B2-10": true, "abc-1": true,
		"": false, "ABC": false, "ABC-": false, "../ABC-1": false, "A/B-1": false, "ABC-1/..": false, "1AB-1": false} {
		if ValidKey(key) != want {
			t.Errorf("ValidKey(%q) = %v", key, !want)
		}
	}
}

// TestCount: Count asks the approximate count for the JQL and reads no issue.
func TestCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ JQL string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/approximate-count" || body.JQL != "labels = x" {
			t.Errorf("%s %s %q", r.Method, r.URL.Path, body.JQL)
		}
		fmt.Fprint(w, `{"count": 42}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	if n, err := c.Count(context.Background(), "labels = x"); n != 42 || err != nil {
		t.Errorf("Count = %d, %v", n, err)
	}
}

// TestSearchCardsTotal: a search the card limit cuts off asks the count for
// its total; one it doesn't, or that ends right at it, asks nothing more.
func TestSearchCardsTotal(t *testing.T) {
	for _, c := range []struct{ limit, issues, total, counts int }{{2, 3, 2100, 1}, {3, 3, 3, 0}, {5, 3, 3, 0}} {
		counts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Token string `json:"nextPageToken"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch r.URL.Path {
			case "/rest/api/3/search/approximate-count":
				counts++
				fmt.Fprint(w, `{"count": 2100}`)
			case "/rest/api/3/search/jql":
				n := 0
				fmt.Sscan(body.Token, &n)
				next := ""
				if n+1 < c.issues {
					next = fmt.Sprint(n + 1)
				}
				fmt.Fprintf(w, `{"issues": [{"key": "ABC-%d", "fields": {}}], "nextPageToken": %q}`, n+1, next)
			default:
				fmt.Fprint(w, `[]`)
			}
		}))
		cl := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", CardLimit: c.limit})
		cards, total, err := cl.SearchCardsTotal(context.Background(), "x")
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) != min(c.limit, c.issues) || total != c.total || counts != c.counts {
			t.Errorf("limit %d: %d cards of %d, %d counts; want %d counts, total %d", c.limit, len(cards), total, counts, c.counts, c.total)
		}
	}
}

// TestSearchCardsPageAndKeys: a page past the card limit carries resolution
// fields; Keys asks for keys only, thousands a page, and pages on.
func TestSearchCardsPageAndKeys(t *testing.T) {
	var asked []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/field" {
			fmt.Fprint(w, `[]`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		asked = append(asked, body)
		switch {
		case fmt.Sprint(body["fields"]) == "[key]" && body["nextPageToken"] == nil:
			fmt.Fprint(w, `{"issues": [{"key": "ABC-1"}, {"key": "../x"}], "nextPageToken": "k2"}`)
		case fmt.Sprint(body["fields"]) == "[key]":
			fmt.Fprint(w, `{"issues": [{"key": "ABC-2"}]}`)
		default:
			fmt.Fprint(w, `{"issues": [{"key": "ABC-1", "fields": {"resolution": {"id": "1", "name": "Won't Do"}, "resolutiondate": "2026-11-02T10:00:00.000+0000"}}], "nextPageToken": "p2"}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok", CardLimit: 50})
	cards, next, err := c.SearchCardsPage(context.Background(), "project = ABC", "")
	if err != nil || next != "p2" || len(cards) != 1 || cards[0].Resolution != "Won't Do" || cards[0].Resolved.Day() != 2 {
		t.Fatalf("SearchCardsPage = %+v, %q, %v", cards, next, err)
	}
	if fmt.Sprint(asked[0]["maxResults"]) != fmt.Sprint(cardPage) {
		t.Errorf("a page asked %v, want %d whatever the card limit", asked[0]["maxResults"], cardPage)
	}
	keys, err := c.Keys(context.Background(), "project = ABC")
	if err != nil || fmt.Sprint(keys) != "[ABC-1 ABC-2]" {
		t.Fatalf("Keys = %v, %v; want both pages, the bad key dropped", keys, err)
	}
	if fmt.Sprint(asked[1]["maxResults"]) != fmt.Sprint(keysPage) || asked[2]["nextPageToken"] != "k2" {
		t.Errorf("keys asked %v then %v", asked[1], asked[2])
	}
}
