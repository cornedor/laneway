package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/autostart"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
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

// TestAutostartAPI: the session says whether it is on; PUT turns it on and off.
func TestAutostartAPI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	sys := autostart.System{OS: "linux", Home: t.TempDir(), LookPath: func(string) (string, error) { return "systemctl", nil }, Run: func(string, ...string) error { return nil }}
	srv := New(context.Background(), Options{AllowedHosts: []string{"example.com"}, Autostart: &Autostart{Sys: sys, Argv: []string{"/bin/laneway", "web"}},
		Setup: &Setup{Save: func(context.Context, SetupForm) (string, error) { return "Ada", nil }}})
	call := func(method, path, body string) string {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Body.String()
	}
	if got := call("GET", "/api/session", ""); !strings.Contains(got, `"autostart":{"enabled":false`) {
		t.Errorf("session = %s", got)
	}
	if got := call("POST", "/api/setup", `{"Site":"acme","Autostart":true}`); !strings.Contains(got, `"autostartError":""`) || !sys.Enabled() {
		t.Errorf("setup = %s, enabled %v", got, sys.Enabled())
	}
	if got := call("PUT", "/api/autostart", `{"On":false}`); !strings.Contains(got, `"enabled":false`) || sys.Enabled() {
		t.Errorf("off = %s", got)
	}
}

// TestAddSite: the session offers the form; POST /api/sites hands it to
// Save with the name, and a refusal names its field.
func TestAddSite(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	st, _ := store.Open(filepath.Join(t.TempDir(), "state.json"))
	var got SetupForm
	var gotName string
	opt := Options{Client: jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}}), Store: st,
		AddSite: &SiteAdder{Keyring: true, Save: func(_ context.Context, name string, f SetupForm) (string, string, error) {
			if name == "taken" {
				return "", "", FieldError{"name", "There is a site taken already."}
			}
			got, gotName = f, name
			return "Ada", "club", nil
		}}}
	ts := httptest.NewServer(New(context.Background(), opt))
	t.Cleanup(ts.Close)
	var sess struct{ AddSite map[string]any }
	if workCall(t, "GET", ts.URL+"/api/session", "", &sess) != 200 || sess.AddSite["keyring"] != true {
		t.Errorf("session addSite = %v", sess.AddSite)
	}
	var e struct{ Fields map[string]string }
	if c := workCall(t, "POST", ts.URL+"/api/sites", `{"Name":"taken","Site":"x"}`, &e); c != 400 || e.Fields["name"] == "" {
		t.Errorf("taken = %d %v", c, e)
	}
	var out map[string]string
	if c := workCall(t, "POST", ts.URL+"/api/sites", `{"Name":"","Site":"club","Email":"a@b.c","Token":"t","Keyring":true,"Demo":true}`, &out); c != 200 || out["site"] != "club" || out["who"] != "Ada" || got.Site != "club" || !got.Keyring || got.Demo || gotName != "" {
		t.Errorf("add = %d %v %+v", c, out, got)
	}
}
