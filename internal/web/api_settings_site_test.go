package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func settingsSite(t *testing.T) *Server {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("ui:\n  card_limit: 5\n"), 0o600)
	o := Options{Site: "demo", Sites: []string{"demo"}, ConfigPath: path}
	o.Open = func(string) (Options, error) { return o, nil }
	return New(t.Context(), o)
}

func call(s *Server, method, url, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1"+url, strings.NewReader(body))
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: siteCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestSettingsSurviveSiteCookie(t *testing.T) {
	s := settingsSite(t)
	if rec := call(s, "PUT", "/api/settings/card_limit", `{"Value":100}`, "s:demo"); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body)
	}
	for _, c := range []string{"s:demo", ""} {
		if got := call(s, "GET", "/api/settings", "", c).Body.String(); !strings.Contains(got, `"Name":"card_limit"`) || !strings.Contains(got, `"Value":100`) {
			t.Errorf("cookie %q: edit lost: %.300s", c, got)
		}
	}
	if s.UIConfig().CardLimit != 100 {
		t.Errorf("UIConfig = %d", s.UIConfig().CardLimit)
	}
}

func TestSettingsRace(t *testing.T) {
	s := settingsSite(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		go func() { defer wg.Done(); call(s, "PUT", "/api/settings/stale_days", `{"Value":`+string(rune('1'+i%9))+`}`, "s:demo") }()
		go func() { defer wg.Done(); call(s, "GET", "/api/ui/actions", "", "s:demo"); call(s, "GET", "/api/settings", "", "") }()
	}
	wg.Wait()
}

func TestSettingsRejectBadThemeAndAction(t *testing.T) {
	s := settingsSite(t)
	if rec := call(s, "PUT", "/api/settings/theme", `{"YAML":"preset: nope"}`, ""); rec.Code != 400 {
		t.Errorf("bad theme preset: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, "PUT", "/api/settings/actions", `{"YAML":"- name: x"}`, ""); rec.Code != 400 {
		t.Errorf("broken action: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, "PUT", "/api/settings/actions", `{"YAML":"- name: x\n  command: [echo]"}`, ""); rec.Code != 200 {
		t.Errorf("good action: %d %s", rec.Code, rec.Body)
	}
}
