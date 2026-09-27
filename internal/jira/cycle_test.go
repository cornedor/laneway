package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCycleTimes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/status":
			io.WriteString(w, `[{"id":"1","statusCategory":{"key":"new"}},{"id":"3","statusCategory":{"key":"indeterminate"}},{"id":"5","statusCategory":{"key":"done"}}]`)
		default:
			io.WriteString(w, `{"issues":[{"key":"A-1","fields":{"summary":"One","created":"2026-09-01T09:00:00.000+0000","resolutiondate":"2026-09-11T09:00:00.000+0000"},
				"changelog":{"histories":[{"created":"2026-09-08T09:00:00.000+0000","items":[{"field":"status","from":"1","to":"3"}]},
				{"created":"2026-09-11T09:00:00.000+0000","items":[{"field":"status","from":"3","to":"5"}]}]}},
				{"key":"A-2","fields":{"summary":"Two","created":"2026-09-01T09:00:00.000+0000","resolutiondate":"2026-09-02T09:00:00.000+0000"},"changelog":{"histories":[]}}]}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.CycleTimes(context.Background(), "A", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "A-2" || got[1].Lead != 240*time.Hour || got[1].Cycle != 72*time.Hour || got[0].Cycle != 0 {
		t.Errorf("got %+v", got)
	}
	ds := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if Percentile(ds, 50) != 5 || Percentile(ds, 85) != 9 || Percentile(nil, 50) != 0 {
		t.Errorf("percentiles %v %v", Percentile(ds, 50), Percentile(ds, 85))
	}
}
