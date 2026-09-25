package ui

import (
	"charm.land/lipgloss/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/config"
)

func TestKeysConfigYAML(t *testing.T) {
	var c config.UIConfig
	if err := yaml.Unmarshal([]byte("keys:\n  search: f\n  mine: [m, M]\n"), &c); err != nil {
		t.Fatal(err)
	}
	if got := c.Keys["search"]; len(got) != 1 || got[0] != "f" {
		t.Errorf("scalar = %v", got)
	}
	if got := c.Keys["mine"]; len(got) != 2 || got[1] != "M" {
		t.Errorf("list = %v", got)
	}
}

func TestRebindKeys(t *testing.T) {
	k := defaultKeys()
	warn := k.applyKeys(map[string]config.KeyList{"search": {"f"}, "bogus": {"x"}, "help": {}})
	if len(warn) != 2 || !strings.Contains(warn[0], "bogus") || !strings.Contains(warn[1], "help") {
		t.Errorf("warnings = %v", warn)
	}
	if keysLabel(k.Search) != "f" || k.Search.Help().Desc != "search" {
		t.Errorf("search = %q %q", keysLabel(k.Search), k.Search.Help().Desc)
	}
	if keysLabel(k.Help) != "?" {
		t.Error("an empty rebind replaced help")
	}
}

// TestReboundKeyDrivesBoard: a rebound action answers on its new key, not
// the old one, and the help overlay shows it.
func TestReboundKeyDrivesBoard(t *testing.T) {
	m := jiraTabModel(t)
	m.keys.applyKeys(map[string]config.KeyList{"search": {"f"}})
	out, _ := m.handleKey(keyMsg(t, "/"))
	if out.(Model).jiraTab.searching {
		t.Error("old key still searches")
	}
	out, _ = m.handleKey(keyMsg(t, "f"))
	if !out.(Model).jiraTab.searching {
		t.Fatal("new key does not search")
	}
	m.helpOpen = true
	if !regexp.MustCompile(`(?m)\bf +search \(esc`).MatchString(ansi.Strip(m.renderHelp(40))) {
		t.Error("help lacks the rebound key")
	}
}

func TestKeyClashes(t *testing.T) {
	k := defaultKeys()
	if warn := k.clashes(); len(warn) != 0 {
		t.Fatalf("defaults clash: %v", warn)
	}
	warn := k.applyKeys(map[string]config.KeyList{"search": {"m"}, "status": {"c"}})
	if len(warn) != 2 ||
		warn[0] != `ui.keys: "m" is both search and mine on the board` ||
		warn[1] != `ui.keys: "c" is both status and comment on the panel` {
		t.Errorf("warnings = %v", warn)
	}
}

func TestKeyScopesNameActions(t *testing.T) {
	k := defaultKeys()
	names := k.keyNames()
	for _, s := range keyScopes {
		for _, a := range s.actions {
			if names[a] == nil {
				t.Errorf("%s scope: unknown action %q", s.name, a)
			}
		}
	}
}

func TestThemeConfigYAML(t *testing.T) {
	var c config.UIConfig
	if err := yaml.Unmarshal([]byte("theme: tokyonight\n"), &c); err != nil || c.Theme["preset"] != "tokyonight" {
		t.Errorf("scalar = %v %v", c.Theme, err)
	}
	c = config.UIConfig{}
	if err := yaml.Unmarshal([]byte("theme:\n  preset: nord\n  accent: \"1\"\n"), &c); err != nil || c.Theme["accent"] != "1" || c.Theme["preset"] != "nord" {
		t.Errorf("map = %v %v", c.Theme, err)
	}
}

// TestHelpFitsHeight: on a short screen the help runs on into more columns
// instead of past the bottom.
func TestHelpFitsHeight(t *testing.T) {
	m := jiraTabModel(t)
	for _, h := range []int{24, 40} {
		if got := lipgloss.Height(m.renderHelp(h)); got > h {
			t.Errorf("height %d: help is %d rows", h, got)
		}
	}
	if !strings.Contains(ansi.Strip(m.renderHelp(24)), "Board (more)") {
		t.Error("a short screen should split the board keys")
	}
}
