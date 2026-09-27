// laneway is a terminal board for Jira: a project's board as swim
// lanes or a list, with the selected issue in a panel on the right.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/ui"
)

// version is set by the release build.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "rules" {
		os.Exit(rulesCmd(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		os.Exit(setupCmd(os.Args[2:], os.Stderr))
	}
	showVersion := flag.Bool("version", false, "print the version and exit")
	cfgPath := flag.String("config", "", "config file (default ~/.config/laneway/config.yaml, then jiratui's and matterbox's)")
	site := flag.String("site", "", "Jira site from the config's sites: (default jira:)")
	flag.Parse()
	if *showVersion {
		fmt.Println("laneway", version)
		return
	}
	if err := run(*cfgPath, *site); err != nil {
		fmt.Fprintln(os.Stderr, "laneway:", err)
		os.Exit(1)
	}
}

func run(cfgPath, site string) error {
	// The config is read again for each run: @ ends the app with another
	// site picked, or to add one, and it starts again there.
	for {
		cfg, path, err := config.Load(cfgPath)
		first := errors.Is(err, config.ErrNoConfig) || err == nil && site == "" && strings.TrimSpace(cfg.Jira.BaseURL) == ""
		if first && interactive() {
			// A first start: ask for the site instead of explaining YAML.
			if site, err = setup(context.Background(), cfgPath, terminalPrompter(strings.Fields(cfg.UI.Open)), signIn); err != nil {
				return err
			}
			fmt.Println()
			continue
		}
		if err != nil {
			return err
		}
		next, end, err := runSite(cfg, path, site)
		switch {
		case err != nil || end == endQuit:
			return err
		case end == endAddSite:
			added, err := setup(context.Background(), path, terminalPrompter(strings.Fields(cfg.UI.Open)), signIn)
			if err == nil {
				site = added
			} else if !errors.Is(err, errCancelled) {
				fmt.Fprintln(os.Stderr, "laneway:", err)
				fmt.Fprint(os.Stderr, "enter goes back to the board… ")
				fmt.Scanln()
			}
		default:
			site = next
		}
	}
}

// How the app ended: quit, to switch site, or to add one.
const (
	endQuit = iota
	endSwitch
	endAddSite
)

// runSite runs the app on site, and says how it ended: the site it was
// left for, or to add one.
func runSite(cfg config.Config, cfgPath, site string) (string, int, error) {
	jc, err := cfg.Site(site)
	if err != nil {
		return "", endQuit, err
	}
	if err := jc.Check(siteName(site)); err != nil {
		return "", endQuit, fmt.Errorf("%w\n`laneway setup` asks for them and checks they work", err)
	}
	path, err := config.SiteStatePath(site)
	if err != nil {
		return "", endQuit, err
	}
	st, err := store.Open(path)
	if err != nil {
		return "", endQuit, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rulesLog := filepath.Join(filepath.Dir(path), "rules.log")
	m := ui.New(ctx, jc, cfg.UI, cfg.Rules, rulesLog, st).WithSites(cfg.SiteNames(), site).WithConfigPath(cfgPath).WithWarnings(cfg.Unknown)
	final, err := tea.NewProgram(m).Run()
	fm, ok := final.(ui.Model)
	if ok {
		fmt.Fprint(os.Stdout, fm.ReleaseImages())
	}
	if err != nil || !ok {
		return "", endQuit, err
	}
	if fm.AddSite() {
		return "", endAddSite, nil
	}
	if next, ok := fm.NextSite(); ok {
		return next, endSwitch, nil
	}
	return "", endQuit, nil
}

func siteName(site string) string {
	if site == "" {
		return "jira"
	}
	return "sites." + site
}
