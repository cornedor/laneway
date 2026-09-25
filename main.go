// laneway is a terminal board for Jira: a project's board as swim
// lanes or a list, with the selected issue in a panel on the right.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	// @ in the app ends it with another site picked; start again there.
	for {
		next, switched, err := runSite(cfg, site)
		if err != nil || !switched {
			return err
		}
		site = next
	}
}

// runSite runs the app on site, and says which site it was left for.
func runSite(cfg config.Config, site string) (string, bool, error) {
	jc, err := cfg.Site(site)
	if err != nil {
		return "", false, err
	}
	if jc.BaseURL == "" || jc.Email == "" || jc.APIToken == "" {
		return "", false, fmt.Errorf("%s: base_url, email and api_token (or JIRA_API_TOKEN for jira:) must be set", siteName(site))
	}
	path, err := config.SiteStatePath(site)
	if err != nil {
		return "", false, err
	}
	st, err := store.Open(path)
	if err != nil {
		return "", false, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rulesLog := filepath.Join(filepath.Dir(path), "rules.log")
	m := ui.New(ctx, jc, cfg.UI, cfg.Rules, rulesLog, st).WithSites(cfg.SiteNames(), site)
	final, err := tea.NewProgram(m).Run()
	fm, ok := final.(ui.Model)
	if ok {
		fmt.Fprint(os.Stdout, fm.ReleaseImages())
	}
	if err != nil || !ok {
		return "", false, err
	}
	next, switched := fm.NextSite()
	return next, switched, nil
}

func siteName(site string) string {
	if site == "" {
		return "jira"
	}
	return "sites." + site
}
