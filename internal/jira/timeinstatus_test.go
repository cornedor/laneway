package jira

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeInStatus(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	at := func(h int) string { return t0.Add(time.Duration(h) * time.Hour).Format(jiraTime) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/A-1":
			io.WriteString(w, `{"fields":{"created":"`+at(0)+`","status":{"name":"Done"}}}`)
		case "/rest/api/3/issue/A-1/changelog":
			fmt.Fprintf(w, `{"isLast":true,"values":[
				{"created":"%s","items":[{"field":"status","fromString":"To Do","toString":"In Progress"}]},
				{"created":"%s","items":[{"field":"labels","toString":"x"}]},
				{"created":"%s","items":[{"field":"status","fromString":"In Progress","toString":"To Do"}]},
				{"created":"%s","items":[{"field":"status","fromString":"To Do","toString":"In Progress"}]},
				{"created":"%s","items":[{"field":"status","fromString":"In Progress","toString":"Done"}]}]}`, at(2), at(3), at(5), at(6), at(10))
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.TimeInStatus(context.Background(), "A-1", t0.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusTime{{"To Do", 3 * time.Hour, 2, false}, {"In Progress", 7 * time.Hour, 2, false}, {"Done", 2 * time.Hour, 1, true}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v", got)
	}
}
