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
