package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
)

func TestSettingRowsCoverConfig(t *testing.T) {
	rows := settingRows(config.UIConfig{StaleDays: 3, CardFields: []string{"type", "points"}, Theme: config.Theme{"preset": "gruvbox"}, Capacity: map[string]float64{"Ada": 5}})
	if n := reflect.TypeFor[config.UIConfig]().NumField(); len(rows) != n {
		t.Fatalf("%d rows for %d options", len(rows), n)
	}
	got := map[string]string{}
	for _, r := range rows {
		if r.def == "" {
			t.Errorf("%s has no default", r.name)
		}
		got[r.name] = r.value
	}
	for name, want := range map[string]string{"stale_days": "3", "card_fields": "type, points", "theme": "gruvbox", "capacity": "1 set", "images": ""} {
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
}

func TestSettingsOverlay(t *testing.T) {
	m := jiraTabModel(t)
	m.uiConfig.BranchTemplate = "{type}/{key}"
	m = m.WithConfigPath("/tmp/laneway.yaml")
	out, _ := m.handleKey(keyMsg(t, ","))
	m = out.(Model)
	if m.settings == nil {
		t.Fatal(", did not open settings")
	}
	view := m.View().Content
	for _, s := range []string{"Settings", "branch_template", "{type}/{key}", "/tmp/laneway.yaml", "↵ edit"} {
		if !strings.Contains(view, s) {
			t.Errorf("view lacks %q", s)
		}
	}
	out, _ = m.handleKey(keyMsg(t, "j"))
	m = out.(Model)
	if m.settings.idx != 1 || m.jiraTab.row != 0 {
		t.Errorf("j: idx %d, board row %d", m.settings.idx, m.jiraTab.row)
	}
	out, _ = m.handleKey(keyMsg(t, "esc"))
	if m = out.(Model); m.settings != nil {
		t.Error("esc left settings open")
	}
}

func TestSettingsEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("jira:\n  email: a@b\nui:\n  stale_days: 5 # red after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := jiraTabModel(t).WithConfigPath(path)
	m.uiConfig.StaleDays = 5
	press := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			out, _ := m.handleKey(keyMsg(t, k))
			m = out.(Model)
		}
	}
	at := func(name string) {
		t.Helper()
		for i, r := range m.settings.rows {
			if r.name == name {
				m.settings.idx = i
				return
			}
		}
		t.Fatalf("no row %s", name)
	}
	press(",")
	at("stale_days")
	press("enter")
	if m.settings.input == nil || m.settings.input.Value() != "5" {
		t.Fatal("enter did not edit the value")
	}
	m.settings.input.SetValue("0x")
	press("enter")
	if m.settings.input == nil || !strings.Contains(m.settings.err, "not a number") {
		t.Fatalf("bad number accepted: %q", m.settings.err)
	}
	m.settings.input.SetValue("-2")
	press("enter")
	if !strings.Contains(m.settings.err, "below 1") {
		t.Fatalf("invalid value accepted: %q", m.settings.err)
	}
	m.settings.input.SetValue("3")
	press("enter")
	if m.settings.input != nil || m.opts.staleDays != 3 || m.status != "saved ui.stale_days" {
		t.Fatalf("save: input %v, stale %d, status %q, err %q", m.settings.input != nil, m.opts.staleDays, m.status, m.settings.err)
	}
	at("card_fields")
	press("enter")
	m.settings.input.SetValue("type, points")
	press("enter")
	if m.opts.fields.assignee || !m.opts.fields.points {
		t.Errorf("card_fields not applied: %+v", m.opts.fields)
	}
	at("keys")
	press("enter")
	if m.settings.input != nil || m.settings.err == "" {
		t.Error("keys edited on one line")
	}
	got, _ := os.ReadFile(path)
	if want := "jira:\n  email: a@b\nui:\n  stale_days: 3 # red after\n  card_fields: [type, points]\n"; string(got) != want {
		t.Errorf("file =\n%s", got)
	}
}
