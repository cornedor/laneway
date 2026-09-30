package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsEdit(t *testing.T) {
	s := New(t.Context(), Options{})
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("# mine\nui:\n  stale_days: 3 # why\n"), 0o600)
	s.opt.ConfigPath = path
	s.opt.UI.StaleDays = 3
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
	for _, st := range list.Settings {
		types[st.Name] = st.Type
		if st.Doc == "" || st.Group == "" {
			t.Errorf("%s: no doc or group", st.Name)
		}
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
