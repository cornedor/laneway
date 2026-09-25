// jiratui is matterbox's Jira tab as its own app: a project's board as swim
// lanes or a list, with the selected issue in a panel on the right.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"jiratui/internal/config"
	"jiratui/internal/store"
	"jiratui/internal/ui"
)

func main() {
	cfgPath := flag.String("config", "", "config file (default ~/.config/jiratui/config.yaml, then matterbox's)")
	flag.Parse()
	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "jiratui:", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if cfg.Jira.BaseURL == "" || cfg.Jira.Email == "" || cfg.Jira.APIToken == "" {
		return fmt.Errorf("jira.base_url, jira.email and jira.api_token (or JIRA_API_TOKEN) must be set")
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	final, err := tea.NewProgram(ui.New(ctx, cfg.Jira, st)).Run()
	if m, ok := final.(ui.Model); ok {
		fmt.Fprint(os.Stdout, m.ReleaseImages())
	}
	return err
}
