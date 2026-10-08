package main

import (
	"flag"
	"fmt"
	"io"
	"text/template"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/ui"
)

var promptUsage = i18n.N(`usage: laneway prompt [-format TEMPLATE]

Prints the git branch's issue, its status, the timer and the inbox count
from the state file, without asking Jira: "ABC-12 · In review · ⏱ 1h 20m · ✉ 3".
Nothing known prints nothing. -format is a Go template over .Key .Status
.TimerKey .Timer .Inbox, e.g. '{{.Key}}{{if .Timer}} {{.Timer}}{{end}}'.`)

// promptCmd prints the prompt segment of the site's state.
func promptCmd(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprintln(errOut, i18n.T(promptUsage)) }
	cfgPath := fs.String("config", "", i18n.T("config file"))
	site := fs.String("site", "", i18n.T("Jira site from the config's sites: (default the one last picked with @)"))
	format := fs.String("format", "", i18n.T("Go template over .Key .Status .TimerKey .Timer .Inbox"))
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var tmpl *template.Template
	if *format != "" {
		t, err := template.New("prompt").Parse(*format)
		if err != nil {
			fmt.Fprintln(errOut, "laneway prompt:", err)
			return 2
		}
		tmpl = t
	}
	if *site == "" {
		if cfg, _, err := config.Load(*cfgPath); err == nil {
			*site = config.LastSite(cfg.SiteNames())
		}
	}
	path, err := config.SiteStatePath(*site)
	if err != nil {
		fmt.Fprintln(errOut, "laneway prompt:", err)
		return 1
	}
	st, err := store.Open(path)
	if err != nil {
		fmt.Fprintln(errOut, "laneway prompt:", err)
		return 1
	}
	p := ui.ReadPrompt(st, time.Now())
	if tmpl != nil {
		if err := tmpl.Execute(out, p); err != nil {
			fmt.Fprintln(errOut, "laneway prompt:", err)
			return 1
		}
		fmt.Fprintln(out)
		return 0
	}
	if s := p.String(); s != "" {
		fmt.Fprintln(out, s)
	}
	return 0
}
