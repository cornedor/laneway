package ui

import (
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
