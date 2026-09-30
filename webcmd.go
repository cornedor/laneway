package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/web"
)

// webCmd is `laneway web` (alias serve): the board in a browser.
func webCmd(args []string, cfgPath, site string, errOut io.Writer) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(errOut)
	addr := fs.String("addr", "127.0.0.1:8484", "address to listen on")
	remote := fs.Bool("remote", false, "allow a non-loopback address (the UI acts as you on Jira)")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	demoFlag := fs.Bool("demo", false, "serve a generated project instead of Jira")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := runWeb(cfgPath, site, *addr, *remote, !*noOpen, *demoFlag); err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	return 0
}

func runWeb(cfgPath, site, addr string, remote, open, demoMode bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opt := web.Options{Version: version, Demo: demoMode}
	var uiCfg config.UIConfig
	var jc config.JiraConfig
	if demoMode {
		baseURL, stopDemo, err := demo.New(time.Now()).Start()
		if err != nil {
			return err
		}
		defer stopDemo()
		dir, err := os.MkdirTemp("", "laneway-web-demo-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		if opt.Store, err = store.Open(filepath.Join(dir, "state.json")); err != nil {
			return err
		}
		jc = config.JiraConfig{BaseURL: baseURL, Email: "demo@example.com", APIToken: "demo", Projects: []string{"DEMO"}}
		opt.Site = "demo"
	} else {
		cfg, _, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		if site == "" {
			site = config.LastSite(cfg.SiteNames())
		}
		if jc, err = cfg.Site(site); err != nil {
			return err
		}
		if err := jc.Check(siteName(site)); err != nil {
			return fmt.Errorf("%w\n`laneway setup` asks for them and checks they work", err)
		}
		path, err := config.SiteStatePath(site)
		if err != nil {
			return err
		}
		if opt.Store, err = store.Open(path); err != nil {
			return err
		}
		uiCfg, opt.Site, opt.Sites = cfg.UI, site, cfg.SiteNames()
	}
	timeout, _ := jc.RequestTimeout()
	opt.Jira, opt.UI = jc, uiCfg
	opt.Client = jira.New(jira.Config{
		BaseURL: jc.BaseURL, Email: jc.Email, APIToken: jc.APIToken, Projects: jc.Projects,
		StoryPointsField: jc.StoryPointsField, CardLimit: uiCfg.CardLimit, Timeout: timeout,
		CustomFields: uiCfg.CustomFields,
	})
	srv := web.New(ctx, opt)
	return web.Serve(ctx, addr, remote, srv, func(a net.Addr) {
		url := "http://" + a.String()
		fmt.Fprintln(os.Stderr, "laneway web on", url)
		if open {
			openBrowser(url)
		}
	})
}

func openBrowser(url string) {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name = "rundll32"
		url = "url.dll,FileProtocolHandler " + url
	}
	_ = exec.Command(name, url).Start()
}
