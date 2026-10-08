package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/store"
)

func TestSettingsEdit(t *testing.T) {
	s := New(t.Context(), Options{})
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("# mine\nui:\n  stale_days: 3 # why\n"), 0o600)
	s.opt.ConfigPath = path
	s.sites.ui.StaleDays = 3
	ts := httptest.NewServer(s)
	defer ts.Close()

	put := func(name, body string) (int, map[string]any) {
		req, _ := http.NewRequest("PUT", ts.URL+"/api/settings/"+name, bytes.NewBufferString(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}

	var list struct {
		Settings []Setting
		Groups   []string
	}
	res, _ := http.Get(ts.URL + "/api/settings")
	json.NewDecoder(res.Body).Decode(&list)
	types := map[string]string{}
	basic := 0
	for _, st := range list.Settings {
		types[st.Name] = st.Type
		if st.Doc == "" || st.Group == "" {
			t.Errorf("%s: no doc or group", st.Name)
		}
		if !st.Advanced {
			basic++
		}
		if want := st.Name == "panel_width" || st.Name == "stale_after" || st.Name == "velocity_sprints" || st.Name == "theme"; want && !st.Advanced || st.Name == "lane_layouts" && st.Advanced {
			t.Errorf("%s: advanced %v", st.Name, st.Advanced)
		}
		if st.Terminal != config.SettingsTerminal[st.Name] || st.Name == "theme" && !st.Terminal {
			t.Errorf("%s: terminal %v", st.Name, st.Terminal)
		}
	}
	if basic != len(config.SettingsBasic) { // a name there that is no option
		t.Errorf("%d basic options, config.SettingsBasic names %d", basic, len(config.SettingsBasic))
	}
	for n, want := range map[string]string{"stale_days": "number", "auto_refresh": "duration", "mouse": "bool", "icons": "enum", "card_fields": "list", "branch_template": "text", "quick_filters": "yaml", "keys": "yaml", "theme": "enum"} {
		if types[n] != want {
			t.Errorf("%s is %q, want %q", n, types[n], want)
		}
	}

	if code, _ := put("stale_days", `{"Value": "x"}`); code != 400 {
		t.Errorf("bad number: %d", code)
	}
	if code, v := put("auto_refresh", `{"Value": "1s"}`); code != 400 || !strings.Contains(v["error"].(string), "duration") {
		t.Errorf("bad duration: %d %v", code, v)
	}
	if code, _ := put("icons", `{"Value": "emoji"}`); code != 400 {
		t.Errorf("bad enum: %d", code)
	}
	if code, _ := put("stale_days", `{"Value": 7}`); code != 200 {
		t.Fatalf("stale_days: %d", code)
	}
	if code, _ := put("card_fields", `{"Value": "type, status"}`); code != 200 {
		t.Fatalf("card_fields: %d", code)
	}
	if code, v := put("keys", `{"YAML": "search: \"/\"\nmine: [m, ctrl+m]"}`); code != 200 {
		t.Fatalf("keys: %d %v", code, v)
	}
	if code, _ := put("keys", `{"YAML": "nonsense: x"}`); code != 400 {
		t.Errorf("unknown action: %d", code)
	}
	raw, _ := os.ReadFile(path)
	got := string(raw)
	for _, want := range []string{"# mine", "stale_days: 7 # why", "card_fields: [type, status]", "- ctrl+m"} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %q:\n%s", want, got)
		}
	}
	if len(s.UIConfig().CardFields) != 2 {
		t.Error("not applied live")
	}
	if code, _ := put("stale_days", `{"Value": ""}`); code != 200 {
		t.Fatal("reset")
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "stale_days") {
		t.Errorf("reset left it:\n%s", raw)
	}
}

// TestUpdate: a newer release than the build (the store's daily look, as the
// TUI keeps it) comes with the upgrade command; a dev build or
// ui.update_check off says nothing.
func TestUpdate(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "state.json"))
	_ = st.SetMeta("release_latest", fmt.Sprintf("%d\tv9.9.9", time.Now().Unix()))
	get := func(o Options) map[string]string {
		o.Store = st
		ts := httptest.NewServer(New(context.Background(), o))
		defer ts.Close()
		var u map[string]string
		if c := workCall(t, "GET", ts.URL+"/api/update", "", &u); c != 200 {
			t.Fatalf("update = %d", c)
		}
		return u
	}
	if u := get(Options{Version: "v0.4.0", UpgradeCmd: "brew upgrade laneway"}); u["Tag"] != "v9.9.9" || u["Command"] != "brew upgrade laneway" || u["Page"] == "" {
		t.Errorf("newer = %v", u)
	}
	if u := get(Options{Version: "dev"}); len(u) != 0 {
		t.Errorf("dev = %v", u)
	}
	if u := get(Options{Version: "v0.4.0", UI: config.UIConfig{UpdateCheck: "off"}}); len(u) != 0 {
		t.Errorf("off = %v", u)
	}
	if u := get(Options{Version: "v9.9.9"}); len(u) != 0 {
		t.Errorf("current = %v", u)
	}
}

func TestSettingsCommandRemote(t *testing.T) {
	s := New(t.Context(), Options{Token: "sekret"})
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("ui:\n"), 0o600)
	s.opt.ConfigPath = path
	ui := reflect.TypeOf(config.UIConfig{})
	for name := range config.SettingsCommand {
		if !fieldByName(reflect.New(ui).Elem(), name).IsValid() {
			t.Errorf("ui.%s: no such setting", name)
		}
	}
	put := func(name, body string) int {
		r := httptest.NewRequest("PUT", "/api/settings/"+name, strings.NewReader(body))
		r.Host = "localhost"
		r.Header.Set("Cookie", tokenCookie+"=sekret")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if c := put("llm", `{"Value": "sh -c id"}`); c != 403 {
		t.Errorf("ui.llm over -remote = %d, want 403", c)
	}
	if c := put("actions", `{"YAML": "- {name: x, command: [id]}"}`); c != 403 {
		t.Errorf("ui.actions over -remote = %d, want 403", c)
	}
	if c := put("stale_days", `{"Value": 4}`); c != 200 {
		t.Errorf("ui.stale_days over -remote = %d, want 200", c)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "llm") || strings.Contains(string(b), "actions") {
		t.Errorf("command setting written:\n%s", b)
	}
}

// Another site's options keep the token, so its guard holds too.
func TestSettingsCommandRemoteOtherSite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("ui:\n"), 0o600)
	o := Options{Token: "sekret", Site: "a", Sites: []string{"a", "b"}, ConfigPath: path}
	o.Open = func(site string) (Options, error) { return Options{Site: site, ConfigPath: path}, nil }
	s := New(t.Context(), o)
	r := httptest.NewRequest("PUT", "/api/settings/actions", strings.NewReader(`{"YAML": "- {name: x, command: [id]}"}`))
	r.Host = "localhost"
	r.Header.Set("Cookie", tokenCookie+"=sekret; "+siteCookie+"=s:b")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("ui.actions on site b over -remote = %d, want 403", w.Code)
	}
}

func TestSiteProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("jira:\n  projects: [ABC]\n"), 0o600)
	s := New(t.Context(), Options{ConfigPath: path, Jira: config.JiraConfig{Projects: []string{"ABC"}}})
	ts := httptest.NewServer(s)
	defer ts.Close()
	put := func(body string) int {
		req, _ := http.NewRequest("PUT", ts.URL+"/api/site/projects", bytes.NewBufferString(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := put(`{"Projects": ["xyz", "ABC", "XYZ"]}`); code != 200 {
		t.Fatalf("status %d", code)
	}
	if got := s.projects(); !reflect.DeepEqual(got, []string{"XYZ", "ABC"}) {
		t.Errorf("projects %v", got)
	}
	if c, _, _ := config.Load(path); !reflect.DeepEqual(c.Jira.Projects, []string{"XYZ", "ABC"}) {
		t.Errorf("config %v", c.Jira.Projects)
	}
	if code := put(`{"Projects": ["no key"]}`); code != http.StatusBadRequest {
		t.Errorf("bad key: status %d", code)
	}
}

func TestRestart(t *testing.T) {
	restarted := false
	ts := httptest.NewServer(New(t.Context(), Options{Restart: func() { restarted = true }}))
	defer ts.Close()
	res, err := http.Post(ts.URL+"/api/restart", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || !restarted {
		t.Errorf("status %d, restarted %v", res.StatusCode, restarted)
	}
	ts2 := httptest.NewServer(New(t.Context(), Options{}))
	defer ts2.Close()
	if res, _ := http.Post(ts2.URL+"/api/restart", "application/json", nil); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("no Restart: status %d", res.StatusCode)
	}
}
