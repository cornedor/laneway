// laneway is a terminal board for Jira: a project's board as swim
// lanes or a list, with the selected issue in a panel on the right.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/ui"
)

// version is set by the release build.
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	cfgPath := flag.String("config", "", "config file (default ~/.config/laneway/config.yaml, then jiratui's and matterbox's)")
	flag.Parse()
	if *showVersion {
		fmt.Println("laneway", version)
		return
	}
	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "laneway:", err)
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
	path, err := config.StatePath()
	if err != nil {
		return err
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	final, err := tea.NewProgram(ui.New(ctx, cfg.Jira, cfg.UI, st)).Run()
	if m, ok := final.(ui.Model); ok {
		fmt.Fprint(os.Stdout, m.ReleaseImages())
	}
	return err
}
