package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

func TestSiteCookieSwitchesClient(t *testing.T) {
	mk := func(name string) (*httptest.Server, Options) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"accountId":"` + name + `","displayName":"` + name + `"}`))
		}))
		return srv, Options{Site: name, Client: jira.New(jira.Config{BaseURL: srv.URL, Email: "a@b.c", APIToken: "x"})}
	}
	a, optA := mk("a")
	b, optB := mk("b")
	defer a.Close()
	defer b.Close()
	optA.Sites = []string{"a", "b"}
	optA.Open = func(n string) (Options, error) { return optB, nil }
	srv := New(context.Background(), optA)
	who := func(cookie string) string {
		req := httptest.NewRequest("GET", "/api/session", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: siteCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if got := who(""); !strings.Contains(got, `"site":"a"`) {
		t.Errorf("default = %s", got)
	}
	if got := who("s:b"); !strings.Contains(got, `"site":"b"`) || !strings.Contains(got, `"AccountID":"b"`) {
		t.Errorf("site b = %s", got)
	}
	if got := who("s:nope"); !strings.Contains(got, `"site":"a"`) {
		t.Errorf("unknown site = %s", got)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/site", strings.NewReader(`{"Site":"zzz"}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("unknown site switch = %d", rec.Code)
	}
}
