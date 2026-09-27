// laneway is a terminal board for Jira: a project's board as swim
// lanes or a list, with the selected issue in a panel on the right.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	showVersion := flag.Bool("version", false, "print the version and exit")
	cfgPath := flag.String("config", "", "config file (default ~/.config/laneway/config.yaml, then jiratui's and matterbox's)")
	site := flag.String("site", "", "Jira site from the config's sites: (default the one last picked with @, else jira:)")
	flag.Parse()
	if *showVersion {
		fmt.Println("laneway", version)
		return
	}
	if flag.NArg() > 0 {
		os.Exit(subcommand(flag.Args(), *cfgPath, *site, os.Stdout, os.Stderr))
	}
	if err := run(*cfgPath, *site); err != nil {
		fmt.Fprintln(os.Stderr, "laneway:", err)
		os.Exit(1)
	}
}

// subcommand runs a command without the board (list, view, create, move,
// rules, setup, prompt or hook), handing on the global -config and -site
// given before it.
func subcommand(args []string, cfgPath, site string, out, errOut io.Writer) int {
	var global []string
	if cfgPath != "" {
		global = append(global, "-config", cfgPath)
	}
	switch args[0] {
	case "rules":
		if len(args) > 1 && site != "" {
			global = append(global, "-site", site)
		}
		if len(args) > 1 {
			return rulesCmd(append([]string{args[1]}, append(global, args[2:]...)...), out, errOut)
		}
		return rulesCmd(nil, out, errOut)
	case "setup":
		return setupCmd(append(global, args[1:]...), errOut)
	case "prompt":
		if site != "" {
			global = append(global, "-site", site)
		}
		return promptCmd(append(global, args[1:]...), out, errOut)
	case "hook":
		if len(args) < 2 {
			return hookCmd(nil, out, errOut)
		}
		if site != "" {
			global = append(global, "-site", site)
		}
		return hookCmd(append([]string{args[1]}, append(global, args[2:]...)...), out, errOut)
	case "completion":
		return completionCmd(args[1:], out, errOut)
	case "__complete":
		return completeCmd(args[1:], out)
	case "list", "view", "create", "move":
		if site != "" {
			global = append(global, "-site", site)
		}
		return cliCmd(args[0], append(global, args[1:]...), out, errOut)
	}
	fmt.Fprintf(errOut, "laneway: unknown command %q (list, view, create, move, rules, setup, prompt, hook, completion)\n", args[0])
	return 2
}

func run(cfgPath, site string) error {
	// Without -site it starts on the site last picked with @.
	remembered := site == ""
	// The config is read again for each run: @ ends the app with another
	// site picked, or to add one, and it starts again there.
	for {
		cfg, path, err := config.Load(cfgPath)
		if remembered && err == nil {
			site, remembered = config.LastSite(cfg.SiteNames()), false
		}
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
				_ = config.SetLastSite(site)
			} else if !errors.Is(err, errCancelled) {
				fmt.Fprintln(os.Stderr, "laneway:", err)
				fmt.Fprint(os.Stderr, "enter goes back to the board… ")
				fmt.Scanln()
			}
		default:
			site = next
			_ = config.SetLastSite(site) // a convenience: failing it only forgets the pick
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
