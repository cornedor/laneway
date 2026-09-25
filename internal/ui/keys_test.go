package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"

	"jiratui/internal/config"
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
	if !regexp.MustCompile(`(?m)\bf +search \(esc`).MatchString(ansi.Strip(m.renderHelp())) {
		t.Error("help lacks the rebound key")
	}
}
