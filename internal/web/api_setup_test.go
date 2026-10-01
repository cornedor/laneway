package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSetupMode: a first-start server answers the session with the setup
// screen's facts, refuses the rest, and hands the form to Save.
func TestSetupMode(t *testing.T) {
	var got SetupForm
	srv := New(context.Background(), Options{AllowedHosts: []string{"example.com"}, Setup: &Setup{ConfigPath: "/c.yaml", Keyring: true, Save: func(_ context.Context, f SetupForm) (string, error) {
		got = f
		if f.Token != "good" {
			return "", FieldError{"token", "Jira did not accept it"}
		}
		return "Ada", nil
	}}})
	call := func(method, path, body string) (int, string) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	if code, body := call("GET", "/api/session", ""); code != 200 || !strings.Contains(body, `"configPath":"/c.yaml"`) || !strings.Contains(body, `"keyring":true`) || !strings.Contains(body, "api-tokens") {
		t.Errorf("session = %d %s", code, body)
	}
	if code, _ := call("GET", "/api/boards/1", ""); code != 503 {
		t.Errorf("board in setup = %d", code)
	}
	if code, body := call("POST", "/api/setup", `{"Site":"acme","Email":"a@b.c","Token":"bad"}`); code != 400 || !strings.Contains(body, `"fields":{"token":"Jira did not accept it"}`) {
		t.Errorf("refused = %d %s", code, body)
	}
	if code, body := call("POST", "/api/setup", `{"Site":"acme","Email":"a@b.c","Token":"good","Keyring":true}`); code != 200 || !strings.Contains(body, `"who":"Ada"`) || !got.Keyring || got.Site != "acme" {
		t.Errorf("saved = %d %s %+v", code, body, got)
	}
}
