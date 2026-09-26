package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// TestShade: with shade auto, the canvas around the cards gets a background
// a step off the terminal's once it reports one (darker on light, lighter
// on dark) while cards keep the terminal's; the bars two steps; off is kept.
func TestShade(t *testing.T) {
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	m := jiraTabModel(t)
	if strings.Contains(m.View().Content, "48;") {
		t.Fatal("shaded before the terminal said its background")
	}
	out, _ := m.Update(tea.BackgroundColorMsg{Color: color.RGBA{0xfa, 0xfa, 0xfa, 0xff}})
	m = out.(Model)
	if !strings.Contains(m.View().Content, "48;2;235;235;235") {
		t.Error("light terminal: no shaded canvas around the cards")
	}
	for _, l := range m.jiraLaneCard(m.jiraTab.cards[2], false, 30) {
		if strings.Contains(l, "48;") {
			t.Errorf("a card is shaded, it should sit on the terminal's background: %q", l)
		}
	}
	if !strings.Contains(bar("x", 3), "48;2;220;220;220") {
		t.Error("light terminal: no bar tone")
	}
	autoShade(color.RGBA{0x10, 0x10, 0x10, 0xff})
	if got := shade("x", 3); !strings.Contains(got, "48;2;30;30;30") {
		t.Errorf("dark terminal: %q", got)
	}
	th, warn := themeFrom(map[string]string{"shade": "off"})
	applyTheme(th)
	autoShade(color.RGBA{0xfa, 0xfa, 0xfa, 0xff})
	if len(warn) != 0 || shade("x", 3) != "x" {
		t.Errorf("off: warn %v, shade %q", warn, shade("x", 3))
	}
}

// TestListZebra: with shading, every other list row is shaded.
func TestListZebra(t *testing.T) {
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	m := jiraTabModel(t)
	out, _ := m.Update(tea.BackgroundColorMsg{Color: color.RGBA{0xfa, 0xfa, 0xfa, 0xff}})
	m = out.(Model)
	out, _ = m.handleKey(keyMsg(t, "t"))
	m = out.(Model)
	var shaded []bool
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansi.Strip(l), "ABC-") {
			shaded = append(shaded, strings.Contains(l, "48;2;235"))
		}
	}
	// The first row is selected; after it the rows alternate.
	if len(shaded) < 4 || !shaded[1] || shaded[2] || !shaded[3] {
		t.Errorf("shaded rows = %v", shaded)
	}
}

// TestChipEndsOnShade: a span with a background of its own (an avatar
// chip) ends on the row's background, shaded or selected, not its own.
func TestChipEndsOnShade(t *testing.T) {
	m := jiraTabModel(t)
	th, _ := themeFrom(map[string]string{"shade": "#1e1e1e"})
	applyTheme(th)
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	chip := jiraAvatar("Ada Lovelace")
	after := func(row, open string) {
		t.Helper()
		i := strings.Index(row, "AL")
		if i < 0 || !strings.Contains(row[i:], diffSoftReset+open) {
			t.Errorf("no %q after the chip: %q", open, row)
		}
	}
	after(shade(chip+" Ada", 20), ansiOpenSeq(shadeStyle))
	after(m.jiraSelect(chip+" Ada", true, 20), ansiOpenSeq(selectedRow))
}

// TestAdaptThemeLight: on a light terminal the default's dark idle selection
// turns light; a dark terminal and a configured colour keep theirs.
func TestAdaptThemeLight(t *testing.T) {
	defer applyTheme(defaultTheme())
	applyTheme(defaultTheme())
	adaptTheme(color.RGBA{0x10, 0x10, 0x10, 0xff})
	if curTheme["selection_idle"] != "238" {
		t.Errorf("dark: selection_idle = %q", curTheme["selection_idle"])
	}
	adaptTheme(color.White)
	if curTheme["selection_idle"] != lightSelectionIdle || diffTreeSelStyle.GetBackground() != lipgloss.Color(lightSelectionIdle) {
		t.Errorf("light: selection_idle = %q", curTheme["selection_idle"])
	}
	th, _ := themeFrom(map[string]string{"selection_idle": "#333333"})
	applyTheme(th)
	adaptTheme(color.White)
	if curTheme["selection_idle"] != "#333333" {
		t.Errorf("configured: selection_idle = %q", curTheme["selection_idle"])
	}
}

// TestStatusColours: unset, the status categories follow dim and the
// roadmap's colours; set, lane marks and lozenges take their own.
func TestStatusColours(t *testing.T) {
	t.Cleanup(func() { applyTheme(defaultTheme()) })
	th, warn := themeFrom(map[string]string{"roadmap_todo": "#010203"})
	applyTheme(th)
	if len(warn) != 0 || !strings.Contains(laneMark["indeterminate"].Render("x"), "1;2;3") {
		t.Errorf("unset: %q %v", laneMark["indeterminate"].Render("x"), warn)
	}
	th, _ = themeFrom(map[string]string{"status_progress": "#040506", "status_todo": "#070809"})
	applyTheme(th)
	if !strings.Contains(laneMark["indeterminate"].Render("x"), "4;5;6") || !strings.Contains(statusLozenge["new"].Render("x"), "7;8;9") {
		t.Errorf("set: %q %q", laneMark["indeterminate"].Render("x"), statusLozenge["new"].Render("x"))
	}
}
