// Package config loads the Jira connection from
// ~/.config/jiratui/config.yaml, falling back to the jira: section of
// matterbox's config so an existing setup just works.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// JiraConfig is matterbox's jira: section.
type JiraConfig struct {
	BaseURL          string            `yaml:"base_url"`
	Email            string            `yaml:"email"`
	APIToken         string            `yaml:"api_token"`
	Projects         []string          `yaml:"projects"`
	StoryPointsField string            `yaml:"story_points_field"`
	Repos            map[string]string `yaml:"repos,omitempty"`
	StartPrompt      string            `yaml:"start_prompt,omitempty"`
}

type Config struct {
	Jira JiraConfig `yaml:"jira"`
	UI   UIConfig   `yaml:"ui"`
}

// UIConfig tunes the app; every field is optional and "" / 0 keeps the
// default (see ui.optionsFrom).
type UIConfig struct {
	// AutoRefresh is how often an idle board refetches ("2m"); "off" stops it.
	AutoRefresh string `yaml:"auto_refresh"`
	// StaleAfter is how old a board may be before focus or a tick refetches it.
	StaleAfter string `yaml:"stale_after"`
	// Images is "auto" (kitty/Ghostty) or "off".
	Images string `yaml:"images"`
	// ImageMaxRows caps an inline image's height in rows.
	ImageMaxRows int `yaml:"image_max_rows"`
	// PanelWidth is the issue panel's share of the width, in percent.
	PanelWidth int `yaml:"panel_width"`
	// Keys rebinds actions by name: search: "/" or mine: [m, M].
	Keys map[string]KeyList `yaml:"keys"`
	// DefaultMode is the board's mode before one is remembered: lanes or list.
	DefaultMode string `yaml:"default_mode"`
	// DateFormat is a Go time layout for the panel's dates.
	DateFormat string `yaml:"date_format"`
	// CardFields picks what cards and list rows show, in any order:
	// type, priority, status, points, assignee, parent.
	CardFields []string `yaml:"card_fields"`
	// QuickFilters are JQL presets shown before every board's own.
	QuickFilters []QuickFilter `yaml:"quick_filters"`
	// Theme overrides colours by name: accent: "#7aa2f7".
	Theme map[string]string `yaml:"theme"`
}

// QuickFilter is a named JQL clause, ANDed with the board's query when on.
type QuickFilter struct {
	Name string `yaml:"name"`
	JQL  string `yaml:"jql"`
}

// KeyList is one key or a list of them.
type KeyList []string

func (k *KeyList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*k = KeyList{n.Value}
		return nil
	}
	var l []string
	if err := n.Decode(&l); err != nil {
		return err
	}
	*k = l
	return nil
}

// Dir is where the config and state live.
func Dir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "jiratui"), nil
}

// Load reads the first config that exists; path "" uses the defaults.
func Load(path string) (Config, string, error) {
	var candidates []string
	if path != "" {
		candidates = []string{path}
	} else {
		d, err := os.UserConfigDir()
		if err != nil {
			return Config{}, "", err
		}
		candidates = []string{
			filepath.Join(d, "jiratui", "config.yaml"),
			filepath.Join(d, "matterbox", "config.yaml"),
		}
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Config{}, p, err
		}
		var c Config
		if err := yaml.Unmarshal(raw, &c); err != nil {
			return Config{}, p, fmt.Errorf("%s: %w", p, err)
		}
		if env := os.Getenv("JIRA_API_TOKEN"); env != "" {
			c.Jira.APIToken = env
		}
		return c, p, nil
	}
	return Config{}, "", fmt.Errorf("no config found; create %s with a jira: section", candidates[0])
}
