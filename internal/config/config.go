// Package config loads the Jira connection from
// ~/.config/laneway/config.yaml, falling back to the old jiratui name and
// then the jira: section of matterbox's config so an existing setup just works.
package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/cornedor/laneway/internal/rules"
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
	// Sites are more Jira instances by name, picked with -site or @ in the
	// app; jira: is the default.
	Sites map[string]JiraConfig `yaml:"sites"`
	UI    UIConfig              `yaml:"ui"`
	// Rules fire on the changes a board refresh shows; see internal/rules.
	Rules []rules.Rule `yaml:"rules"`
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
	// CardLimit caps the cards one view fetches.
	CardLimit int `yaml:"card_limit"`
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
	// Views are JQL-narrowed views of every board, after its own.
	Views []QuickFilter `yaml:"views"`
	// SavedFilters is "on" (your starred Jira filters as views, after
	// Views) or "off".
	SavedFilters string `yaml:"saved_filters"`
	// Theme is a preset name (theme: tokyonight) or colours by name, over
	// an optional preset: {preset: nord, accent: "#7aa2f7"}.
	Theme Theme `yaml:"theme"`
}

// QuickFilter is a named JQL clause, ANDed with the board's query: a quick
// filter when on, a view always.
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

// Theme is colours by name; a lone scalar is {preset: <name>}.
type Theme map[string]string

func (t *Theme) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*t = Theme{"preset": n.Value}
		return nil
	}
	var m map[string]string
	if err := n.Decode(&m); err != nil {
		return err
	}
	*t = m
	return nil
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
			filepath.Join(d, "laneway", "config.yaml"),
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
		if filepath.Base(filepath.Dir(p)) == "matterbox" {
			// Its rules: are chat rules; only jira: and ui: carry over.
			var mb struct {
				Jira JiraConfig `yaml:"jira"`
				UI   UIConfig   `yaml:"ui"`
			}
			err = yaml.Unmarshal(raw, &mb)
			c.Jira, c.UI = mb.Jira, mb.UI
		} else {
			err = yaml.Unmarshal(raw, &c)
		}
		if err != nil {
			return Config{}, p, fmt.Errorf("%s: %w", p, err)
		}
		if env := os.Getenv("JIRA_API_TOKEN"); env != "" {
			c.Jira.APIToken = env
		}
		return c, p, nil
	}
	return Config{}, "", fmt.Errorf("no config found; create %s with a jira: section", candidates[0])
}

// StatePath is where the app keeps its state. A state file left by the old
// jiratui name is copied over on first run.
// Site is the Jira config for site: jira: for "", else sites[site].
func (c Config) Site(site string) (JiraConfig, error) {
	if site == "" {
		return c.Jira, nil
	}
	j, ok := c.Sites[site]
	if !ok {
		return JiraConfig{}, fmt.Errorf("no site %q in sites:", site)
	}
	return j, nil
}

// SiteNames are the sites to switch between, "" (jira:) first, then by name.
func (c Config) SiteNames() []string {
	names := slices.Sorted(maps.Keys(c.Sites))
	return append([]string{""}, names...)
}

// SiteStatePath is StatePath for site: its own file, as boards, views and
// caches are per instance.
func SiteStatePath(site string) (string, error) {
	p, err := StatePath()
	if err != nil || site == "" {
		return p, err
	}
	return filepath.Join(filepath.Dir(p), "state-"+site+".json"), nil
}

func StatePath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, "laneway", "state.json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if old, err := os.ReadFile(filepath.Join(d, "jiratui", "state.json")); err == nil {
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
				_ = os.WriteFile(p, old, 0o600)
			}
		}
	}
	return p, nil
}
