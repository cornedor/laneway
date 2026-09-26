package ui

import (
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
	for _, s := range []string{"Settings", "branch_template", "{type}/{key}", "/tmp/laneway.yaml"} {
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
