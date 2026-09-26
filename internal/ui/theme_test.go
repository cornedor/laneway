package ui

import (
	tea "charm.land/bubbletea/v2"
	"image/color"
	"strings"
	"testing"
)

func TestThemeFrom(t *testing.T) {
	th, warn := themeFrom(map[string]string{"accent": "#7aa2f7", "dim": "244", "nope": "1", "error": "red", "link": "300"})
	if th["accent"] != "#7aa2f7" || th["dim"] != "244" {
		t.Errorf("theme = %v", th)
	}
	if th["error"] != defaultTheme()["error"] || th["link"] != defaultTheme()["link"] {
		t.Error("bad values replaced defaults")
	}
	if len(warn) != 3 {
		t.Errorf("warnings = %v, want 3", warn)
	}
}

func TestApplyThemeRecolours(t *testing.T) {
	defer applyTheme(defaultTheme())
	th, _ := themeFrom(map[string]string{"accent": "#ff0000", "priority_high": "#00ff00", "type_bug": "#0000ff"})
	applyTheme(th)
	if got := jiraKeyStyle.Render("ABC-1"); !strings.Contains(got, "255;0;0") {
		t.Errorf("key style = %q, want red", got)
	}
	if got := jiraPriorityMark("High"); !strings.Contains(got, "0;255;0") {
		t.Errorf("priority mark = %q, want green", got)
	}
	if got := jiraTypeIcon("Bug"); !strings.Contains(got, "0;0;255") {
		t.Errorf("bug icon = %q, want blue", got)
	}
}

func TestThemePresets(t *testing.T) {
	def := defaultTheme()
	for name, p := range themePresets {
		if len(p) != len(def) {
			t.Errorf("%s: %d colours, want %d", name, len(p), len(def))
		}
		th, warn := themeFrom(map[string]string{"preset": name})
		if len(warn) != 0 {
			t.Errorf("%s: warnings %v", name, warn)
		}
		for k := range p {
			if _, ok := def[k]; !ok {
				t.Errorf("%s: unknown colour %q", name, k)
			}
			if th[k] != p[k] {
				t.Errorf("%s: %s = %q, want %q", name, k, th[k], p[k])
			}
		}
	}
	th, _ := themeFrom(map[string]string{"preset": "gruvbox", "accent": "3"})
	if th["accent"] != "3" || th["dim"] != "#928374" {
		t.Errorf("override over preset = %v", th)
	}
	if _, warn := themeFrom(map[string]string{"preset": "nope"}); len(warn) != 1 {
		t.Errorf("unknown preset warnings = %v", warn)
	}
}

// TestShade: with shade auto, cards get a background a step off the
// terminal's once it reports one (darker on light, lighter on dark); off
// and a fixed colour are kept.
func TestShade(t *testing.T) {
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	m := jiraTabModel(t)
	if strings.Contains(m.View().Content, "48;") {
		t.Fatal("shaded before the terminal said its background")
	}
	out, _ := m.Update(tea.BackgroundColorMsg{Color: color.RGBA{0xfa, 0xfa, 0xfa, 0xff}})
	m = out.(Model)
	if !strings.Contains(m.View().Content, "48;2;237;237;237") {
		t.Error("light terminal: no darker shade on the cards")
	}
	autoShade(color.RGBA{0x10, 0x10, 0x10, 0xff})
	if got := shade("x", 3); !strings.Contains(got, "48;2;32;32;32") {
		t.Errorf("dark terminal: %q", got)
	}
	th, warn := themeFrom(map[string]string{"shade": "off"})
	applyTheme(th)
	autoShade(color.RGBA{0xfa, 0xfa, 0xfa, 0xff})
	if len(warn) != 0 || shade("x", 3) != "x" {
		t.Errorf("off: warn %v, shade %q", warn, shade("x", 3))
	}
}
