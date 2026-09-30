package ui

import (
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2/styles"
	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/config"
)

// What the settings overlay says about an option: a line on what it does,
// and for one with a fixed set of values, those to pick from.

var settingDocs = config.SettingDocs

// settingChoices are the values an option with a fixed set takes, the
// default first.
func settingChoices(name string) []string {
	switch name {
	case "code_theme":
		return styles.Names()
	case "theme":
		names := make([]string, 0, len(themePresets))
		for n := range themePresets {
			names = append(names, n)
		}
		slices.Sort(names)
		return names
	}
	return config.SettingChoices(name)
}

// settingFull is an option's whole value as YAML, for one that doesn't
// fit a line; "" when it is unset or fits.
func settingFull(c config.UIConfig, name string) string {
	f := settingField(&c, name)
	if !f.IsValid() || editable(f) || f.IsZero() {
		return ""
	}
	b, err := yaml.Marshal(f.Interface())
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}

// SettingChoices is settingChoices for other front ends (the web settings).
func SettingChoices(name string) []string { return settingChoices(name) }

// ValidateUI reports what optionsFrom objects to in c, as at startup.
func ValidateUI(c config.UIConfig) []string {
	_, warn := optionsFrom(c)
	k := defaultKeys()
	return append(warn, k.applyKeys(c.Keys)...)
}

// KeyActions are the rebindable actions of ui.keys with their default keys.
func KeyActions() map[string][]string {
	k := defaultKeys()
	out := map[string][]string{}
	for name, b := range k.keyNames() {
		out[name] = b.Keys()
	}
	return out
}
