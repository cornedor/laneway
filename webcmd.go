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
	"github.com/cornedor/laneway/internal/offline"
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
		jc := config.JiraConfig{BaseURL: baseURL, Email: "demo@example.com", APIToken: "demo", Projects: []string{"DEMO"}}
		opt.Site, opt.Jira = "demo", jc
		opt.ConfigPath = filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(opt.ConfigPath, []byte("# demo config\n"), 0o600); err != nil {
			return err
		}
		opt.Client = webClient(jc, config.UIConfig{})
		opt.Client.SetQueue(offline.To(opt.Store))
	} else {
		cfg, loaded, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		opt.ConfigPath = loaded
		if site == "" {
			site = config.LastSite(cfg.SiteNames())
		}
		if opt, err = webSite(cfg, site, opt); err != nil {
			return err
		}
		opt.Open = func(other string) (web.Options, error) { return webSite(cfg, other, web.Options{ConfigPath: loaded}) }
		opt.Sites = cfg.SiteNames()
	}
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

// webSite opens a configured site for the server: its client and state.
func webSite(cfg config.Config, site string, opt web.Options) (web.Options, error) {
	jc, err := cfg.Site(site)
	if err != nil {
		return opt, err
	}
	if err := jc.Check(siteName(site)); err != nil {
		return opt, fmt.Errorf("%w\n`laneway setup` asks for them and checks they work", err)
	}
	path, err := config.SiteStatePath(site)
	if err != nil {
		return opt, err
	}
	if opt.Store, err = store.Open(path); err != nil {
		return opt, err
	}
	opt.Site, opt.Jira, opt.UI = site, jc, cfg.UI
	opt.Rules, opt.RulesTest = cfg.Rules, cfg.RulesTest
	opt.Client = webClient(jc, cfg.UI)
	opt.Client.SetQueue(offline.To(opt.Store))
	return opt, nil
}

func webClient(jc config.JiraConfig, uiCfg config.UIConfig) *jira.Client {
	timeout, _ := jc.RequestTimeout()
	return jira.New(jira.Config{
		BaseURL: jc.BaseURL, Email: jc.Email, APIToken: jc.APIToken, Projects: jc.Projects,
		StoryPointsField: jc.StoryPointsField, CardLimit: uiCfg.CardLimit, Timeout: timeout,
		CustomFields: uiCfg.CustomFields,
	})
}
