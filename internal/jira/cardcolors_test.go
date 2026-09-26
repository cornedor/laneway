package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCardColors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/greenhopper/1.0/rapidviewconfig/editmodel.json":
			if r.URL.Query().Get("rapidViewId") != "7" {
				t.Errorf("url = %s", r.URL)
			}
			w.Write([]byte(`{"cardColorConfig":{"rapidViewId":7,"cardColorStrategy":"issuetype"}}`))
		case "/rest/greenhopper/1.0/cardcolors/7/strategy/issuetype":
			w.Write([]byte(`{"cardColorStrategy":"issuetype","cardColors":[
				{"value":"10001","displayValue":"Bug","color":"#ff5630"},{"value":"10002","displayValue":"Story","color":"#36b37e"},{"value":"x","color":""}]}`))
		default:
			t.Errorf("url = %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cc, err := c.CardColors(context.Background(), 7)
	if err != nil || cc.By != "issuetype" || len(cc.Colors) != 2 || cc.Colors[0] != (CardColor{"Bug", "#ff5630"}) {
		t.Errorf("cc = %+v %v", cc, err)
	}
}

// TestCardColorKeys: each custom colour's JQL, within the scope, finds its
// issues; the first colour to take one wins.
func TestCardColorKeys(t *testing.T) {
	var jqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			JQL string `json:"jql"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		jqls = append(jqls, body.JQL)
		if len(jqls) == 1 {
			w.Write([]byte(`{"issues":[{"key":"ABC-1"}]}`))
		} else {
			w.Write([]byte(`{"issues":[{"key":"ABC-1"},{"key":"ABC-2"}]}`))
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	cc := CardColors{By: "custom", Colors: []CardColor{{"priority = High", "#f00"}, {"labels = ui", "#0f0"}}}
	keys, err := c.CardColorKeys(context.Background(), cc, `project = "ABC"`)
	if err != nil || keys["ABC-1"] != "#f00" || keys["ABC-2"] != "#0f0" {
		t.Errorf("keys = %v %v", keys, err)
	}
	if len(jqls) != 2 || jqls[0] != `(project = "ABC") AND (priority = High)` {
		t.Errorf("jql = %q", jqls)
	}
}
