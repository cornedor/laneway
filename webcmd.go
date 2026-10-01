package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cornedor/laneway/internal/autostart"
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
	remote := fs.Bool("remote", false, "allow a non-loopback address; needs the printed ?token= URL. WARNING: grants shell access via ui.actions and ui.llm to whoever holds the token")
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
	token := ""
	if remote {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		token = hex.EncodeToString(b)
	}
	// The setup screen ends by serving the app in its place, on the same
	// address: the browser reloads into it.
	for {
		next, err := serveWeb(ctx, cfgPath, site, addr, token, remote, open, demoMode)
		if errors.Is(err, syscall.EADDRINUSE) && runningWeb(addr) {
			// Started at login, most likely: open that one.
			url := "http://" + addr
			fmt.Fprintln(os.Stderr, "laneway web already runs on", url)
			if open {
				openBrowser(url)
			}
			return nil
		}
		if err != nil || next == webQuit || ctx.Err() != nil {
			return err
		}
		open, demoMode = false, next == webDemo
	}
}

// How a web server ended: stopped, or set up (to the site, or the demo).
const (
	webQuit = iota
	webSetUp
	webDemo
)

func serveWeb(parent context.Context, cfgPath, site, addr, token string, remote, open, demoMode bool) (int, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	end := webQuit
	opt := web.Options{Version: version, Demo: demoMode, Token: token}
	if demoMode {
		baseURL, stopDemo, err := demo.New(time.Now()).Start()
		if err != nil {
			return end, err
		}
		defer stopDemo()
		dir, err := os.MkdirTemp("", "laneway-web-demo-")
		if err != nil {
			return end, err
		}
		defer os.RemoveAll(dir)
		if opt.Store, err = store.Open(filepath.Join(dir, "state.json")); err != nil {
			return end, err
		}
		jc := config.JiraConfig{BaseURL: baseURL, Email: "demo@example.com", APIToken: "demo", Projects: []string{"DEMO"}}
		opt.Site, opt.Jira = "demo", jc
		opt.ConfigPath = filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(opt.ConfigPath, []byte("# demo config\n"), 0o600); err != nil {
			return end, err
		}
		var uiCfg config.UIConfig
		if cfgPath != "" { // as in the TUI's demo: the config's ui: and rules: apply, never its sites
			cfg, _, err := config.Load(cfgPath)
			if err != nil {
				return end, err
			}
			uiCfg, opt.Rules, opt.RulesTest = cfg.UI, cfg.Rules, cfg.RulesTest
		}
		opt.UI = uiCfg
		opt.Client = webClient(jc, uiCfg)
		opt.Client.SetQueue(offline.To(opt.Store))
	} else {
		cfg, loaded, err := config.Load(cfgPath)
		if err != nil && !errors.Is(err, config.ErrNoConfig) {
			return end, err
		}
		opt.ConfigPath = loaded
		if site == "" {
			site = config.LastSite(cfg.SiteNames())
		}
		if st, ok := webSetup(cfg, loaded, site, err != nil); ok {
			st.Save = func(ctx context.Context, f web.SetupForm) (string, error) {
				who, err := webSetupSave(ctx, loaded, site, f)
				if err == nil {
					end = webSetUp
					if f.Demo {
						end = webDemo
					}
					time.AfterFunc(300*time.Millisecond, cancel) // after the answer is sent
				}
				return who, err
			}
			opt.Setup = st
		} else {
			if opt, err = webSite(cfg, site, opt); err != nil {
				return end, err
			}
			opt.Open = func(other string) (web.Options, error) { return webSite(cfg, other, web.Options{ConfigPath: loaded}) }
			opt.Sites = cfg.SiteNames()
			opt.DefaultName = cfg.Jira.Name
		}
	}
	if !demoMode && !remote { // -remote's token changes each start: not for a login service
		opt.Autostart = webAutostart(cfgPath, addr)
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		opt.AllowedHosts = append(opt.AllowedHosts, allowedHosts(host)...)
	}
	srv := web.New(ctx, opt)
	err := web.Serve(ctx, addr, remote, srv, func(a net.Addr) {
		url := "http://" + a.String()
		if opt.Token != "" {
			url += "/?token=" + opt.Token
		}
		switch {
		case opt.Setup != nil:
			fmt.Fprintln(os.Stderr, "laneway web on", url, "· connect it to Jira there")
		default:
			fmt.Fprintln(os.Stderr, "laneway web on", url)
		}
		if open {
			openBrowser(url)
		}
	})
	return end, err
}

// runningWeb is whether a laneway web answers on addr.
func runningWeb(addr string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/api/session")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&v) == nil && v.Version != ""
}

// webAutostart is the login service for this laneway web: this binary (by
// its PATH name when that is the same file, so an upgrade keeps working),
// the same address and -config, and this shell's PATH for the tools it runs.
func webAutostart(cfgPath, addr string) *web.Autostart {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if p, err := exec.LookPath("laneway"); err == nil && sameFile(p, exe) {
		exe = p
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	argv := []string{exe, "web", "-no-open", "-addr", addr}
	if cfgPath != "" {
		if abs, err := filepath.Abs(cfgPath); err == nil {
			argv = append(argv, "-config", abs)
		}
	}
	return &web.Autostart{Sys: autostart.Default(), Argv: argv, Path: os.Getenv("PATH")}
}

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// webSetup is the setup screen for a start without a site to use: no
// config (noConfig), no jira: and no site picked, or a site without its
// email or token (filled in but those).
func webSetup(cfg config.Config, path, site string, noConfig bool) (*web.Setup, bool) {
	st := &web.Setup{Name: site, ConfigPath: path, EnvToken: os.Getenv("JIRA_API_TOKEN") != ""}
	st.Keyring = keyringAvailable()
	if noConfig || site == "" && strings.TrimSpace(cfg.Jira.BaseURL) == "" {
		return st, true
	}
	j, err := cfg.Site(site)
	if err != nil || j.Check(siteName(site)) == nil {
		return nil, false // an unknown -site says so as before
	}
	st.Prefill = web.SetupForm{Site: j.BaseURL, Email: j.Email}
	return st, true
}

// keyringAvailable is whether keyringFor finds a keyring tool.
func keyringAvailable() bool {
	store, _ := keyringFor("https://example.atlassian.net", "you@example.com")
	return store != nil
}

// sentence capitalises msg and ends it with a full stop.
func sentence(msg string) string {
	if msg == "" {
		return msg
	}
	return strings.ToUpper(msg[:1]) + strings.TrimSuffix(msg[1:], ".") + "."
}

// webSetupSave is the setup screen's Connect: as laneway setup, it signs in
// to check the site, then writes it to the config at path as site ("" is
// jira:), the token in the keyring when asked and one is there.
func webSetupSave(ctx context.Context, path, site string, f web.SetupForm) (string, error) {
	if f.Demo {
		return "", nil
	}
	base, err := config.BaseURL(f.Site)
	if err != nil {
		return "", web.FieldError{Field: "site", Msg: sentence(err.Error())}
	}
	j := config.JiraConfig{BaseURL: base, Email: strings.TrimSpace(f.Email), APIToken: strings.TrimSpace(f.Token)}
	if !strings.Contains(j.Email, "@") {
		return "", web.FieldError{Field: "email", Msg: "Type the email address you sign in to Jira with."}
	}
	check := j
	check.APIToken = cmp.Or(j.APIToken, os.Getenv("JIRA_API_TOKEN"))
	if check.APIToken == "" {
		return "", web.FieldError{Field: "token", Msg: "Paste the API token."}
	}
	who, err := signIn(ctx, check)
	switch {
	case err == nil:
	case errors.Is(err, jira.ErrUnauthorized):
		return "", web.FieldError{Field: "token", Msg: "Jira did not accept this email and token. Check the email is the one you sign in with, and copy the token again."}
	case errors.Is(err, jira.ErrNotFound):
		return "", web.FieldError{Field: "site", Msg: base + " answers, but not as Jira Cloud. Check the address."}
	default:
		return "", web.FieldError{Field: "site", Msg: "Could not reach " + base + ": " + err.Error()}
	}
	if f.Keyring && j.APIToken != "" {
		if store, lookup := keyringFor(j.BaseURL, j.Email); store != nil && keyringStore(store, j.APIToken) == nil {
			j.APIToken, j.APITokenCmd = "", lookup
		}
	}
	if err := config.SetSite(path, site, j); err != nil {
		return "", err
	}
	return who, nil
}

// allowedHosts are the Host headers a browser may use for a server bound to
// host: the host itself, or for a wildcard bind this machine's addresses.
func allowedHosts(host string) []string {
	if ip := net.ParseIP(host); host != "" && (ip == nil || !ip.IsUnspecified()) {
		return []string{host}
	}
	var out []string
	if h, err := os.Hostname(); err == nil {
		out = append(out, h)
	}
	if as, err := net.InterfaceAddrs(); err == nil {
		for _, a := range as {
			if ipn, ok := a.(*net.IPNet); ok {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
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
		CustomFields: uiCfg.CustomFields, FlagValue: uiCfg.FlagValue,
	})
}
